// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package client

import (
	"image"
	"io"
	"log"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/damage"
	"github.com/jezek/xgb/xproto"
)

// A mirrored X11 window is captured when something is drawn on it, not on a
// timer. The X server's Damage extension reports every rectangle drawn on the
// screen: one connection for the whole process watches the root window (which
// reports what is drawn in every window on it, a menu or another window laid
// over the mirrored one included), and each window stream is woken when a
// rectangle touches its own. A still window costs nothing between frames, and
// a change it makes on its own is captured at once.
//
// Without it (Wayland, no X server reachable, a server without Damage) a
// stream captures on its timer as before.

var x11Damage struct {
	mu   sync.Mutex
	conn *xgb.Conn
	subs map[*winDamage]struct{}
	// failedAt: the last connect that failed; another is not tried for
	// x11DamageRetry, so a display without Damage does not cost a connect
	// per window opened.
	failedAt time.Time
}

const x11DamageRetry = 30 * time.Second

func init() {
	// xgb logs to stderr by default (a missing ~/.Xauthority, say): not
	// reminal's output.
	xgb.Logger = log.New(io.Discard, "", 0)
}

// winDamage is one stream's interest in a rectangle of the screen.
type winDamage struct {
	wake chan struct{}
	mu   sync.Mutex
	rect image.Rectangle // on the screen; whole, for the desktop
	full bool
	// lost: the connection went away. The stream drops this and captures on
	// its timer.
	lost bool
}

// newWinDamage watches w's rectangle of the screen, or nil when the display
// has no Damage to watch.
func newWinDamage(b windowBackend, w winInfo) *winDamage {
	if _, ok := b.(linuxWindows); !ok || isWaylandSession() {
		return nil
	}
	x11Damage.mu.Lock()
	defer x11Damage.mu.Unlock()
	if x11Damage.conn == nil {
		if !x11Damage.failedAt.IsZero() && time.Since(x11Damage.failedAt) < x11DamageRetry {
			return nil
		}
		c, err := x11DamageConnect()
		if err != nil {
			x11Damage.failedAt = time.Now()
			return nil
		}
		x11Damage.conn, x11Damage.subs = c, map[*winDamage]struct{}{}
		go x11DamageEvents(c)
	}
	d := &winDamage{wake: make(chan struct{}, 1)}
	d.setRect(w)
	x11Damage.subs[d] = struct{}{}
	return d
}

func x11DamageConnect() (*xgb.Conn, error) {
	c, err := xgb.NewConn()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*xgb.Conn, error) { c.Close(); return nil, err }
	if err := damage.Init(c); err != nil {
		return fail(err)
	}
	// The version must be asked before Damage is used (its spec).
	if _, err := damage.QueryVersion(c, 1, 1).Reply(); err != nil {
		return fail(err)
	}
	id, err := damage.NewDamageId(c)
	if err != nil {
		return fail(err)
	}
	root := xproto.Setup(c).DefaultScreen(c).Root
	// Raw rectangles: an event per drawing, and no damage kept by the
	// server, so nothing has to be cleared and nothing drawn between a report
	// and a clear can go unreported.
	if err := damage.CreateChecked(c, id, xproto.Drawable(root), damage.ReportLevelRawRectangles).Check(); err != nil {
		return fail(err)
	}
	return c, nil
}

// x11DamageEvents wakes every stream a drawn rectangle touches, until the
// connection ends; then every stream is told, and captures on its timer.
func x11DamageEvents(c *xgb.Conn) {
	for {
		ev, err := c.WaitForEvent()
		if ev == nil && err == nil {
			break
		}
		n, ok := ev.(damage.NotifyEvent)
		if !ok {
			continue
		}
		r := image.Rect(int(n.Area.X), int(n.Area.Y), int(n.Area.X)+int(n.Area.Width), int(n.Area.Y)+int(n.Area.Height))
		x11Damage.mu.Lock()
		for d := range x11Damage.subs {
			if d.touches(r) {
				d.raise()
			}
		}
		x11Damage.mu.Unlock()
	}
	x11Damage.mu.Lock()
	if x11Damage.conn == c {
		for d := range x11Damage.subs {
			d.mu.Lock()
			d.lost = true
			d.mu.Unlock()
			d.raise()
		}
		x11Damage.conn, x11Damage.subs = nil, nil
		x11Damage.failedAt = time.Now()
	}
	x11Damage.mu.Unlock()
}

func (d *winDamage) touches(r image.Rectangle) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.full || d.rect.Overlaps(r)
}

func (d *winDamage) raise() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// setRect follows the window's rectangle on the screen as it moves.
func (d *winDamage) setRect(w winInfo) {
	d.mu.Lock()
	d.full = isDisplayID(w.ID)
	d.rect = image.Rect(w.X, w.Y, w.X+w.W, w.Y+w.H)
	d.mu.Unlock()
}

// C is raised when something was drawn on the window since it was last read.
func (d *winDamage) C() <-chan struct{} { return d.wake }

// ok reports whether the watch is still live.
func (d *winDamage) ok() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.lost
}

// close ends the watch; the last one closes the connection.
func (d *winDamage) close() {
	x11Damage.mu.Lock()
	defer x11Damage.mu.Unlock()
	if x11Damage.subs == nil {
		return
	}
	delete(x11Damage.subs, d)
	if len(x11Damage.subs) == 0 && x11Damage.conn != nil {
		c := x11Damage.conn
		x11Damage.conn, x11Damage.subs = nil, nil
		// The events goroutine sees the close and ends; it no longer owns
		// the hub, so it leaves it alone.
		c.Close()
	}
}
