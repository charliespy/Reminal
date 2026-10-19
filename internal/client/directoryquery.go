// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"reminal/internal/config"
	"reminal/internal/crypto"
	"reminal/internal/protocol"
)

// ErrDirUnreachable means the machine's directory channel isn't being served —
// the machine is offline, or no session is running to host it. Callers render
// this as an offline machine rather than a hard failure.
var ErrDirUnreachable = errors.New("machine's directory channel is not reachable")

// DirectoryTimeout bounds a single machine's directory query end-to-end.
const DirectoryTimeout = 6 * time.Second

// maxDirMessageBytes caps a single WS frame read from the (untrusted) relay on a
// directory connection, matching the relay's own 1 MB limit. A legit dir_resp is
// a few KB even with hundreds of sessions; anything larger is hostile.
const maxDirMessageBytes = 1 << 20

// QueryDirectory reaches the machine identified by machinePub over its
// owner-derived directory channel, proves this device is an enrolled owner with
// the same signed handshake used for a PIN-free connect, and returns the
// machine's live sessions. The reply is end-to-end encrypted; the relay only
// ever sees opaque frames on an opaque channel id.
// dirBusyError is a machine's answer to a request over its allowance:
// asked again after Wait, it is taken. Never "unreachable": it answered.
type dirBusyError struct{ Wait time.Duration }

func (e *dirBusyError) Error() string { return "the machine is busy; asked again shortly" }

// dirBusyMaxWait caps a wait a "busy" asks for. own_busy travels in the
// clear, so its retry_ms is the relay's to write: whatever it says, a call
// waits no longer than this before asking again.
const dirBusyMaxWait = 60 * time.Second

func busyAfter(ms int) error {
	return &dirBusyError{Wait: min(time.Duration(max(1, ms))*time.Millisecond, dirBusyMaxWait)}
}

// dirBusyTries bounds how many times one call is asked again of a machine
// that keeps saying busy: an abort cap, not a timeout — each wait is the
// machine's own.
const dirBusyTries = 30

// withDirBusy runs one directory request, and while the machine answers
// "busy", waits exactly what it asks and asks again.
func withDirBusy[T any](f func() (T, error)) (T, error) {
	for i := 0; ; i++ {
		v, err := f()
		var busy *dirBusyError
		if !errors.As(err, &busy) || i+1 >= dirBusyTries {
			return v, err
		}
		time.Sleep(busy.Wait)
	}
}

func QueryDirectory(machinePub ed25519.PublicKey, timeout time.Duration) (protocol.DirResponse, error) {
	return queryDirectory(machinePub, timeout, dirQueryReq{})
}

// dirQueryReq is the optional encrypted TypeDirQuery payload. Older hosts
// ignore Data and still return the session list.
type dirQueryReq struct {
	Pattern    string `json:"pattern,omitempty"`
	Transcript string `json:"transcript,omitempty"` // session id to dump
	KeysID     string `json:"keys_id,omitempty"`    // session id to type into
	Keys       string `json:"keys,omitempty"`       // base64 of PTY bytes
	// Restart asks the host to hot-restart every session on it. It rides the
	// query rather than taking a message type of its own so it needs no relay
	// forwarding change — and dir_query is already reserved to the machine
	// channel (dirChannelOnly), so it is owner-only by construction.
	Restart bool `json:"restart,omitempty"`
	// OpenID names one exposed port whose public link and gate PIN the caller
	// wants back (DirResponse.OpenURL/OpenPIN). Asked per click rather than
	// carried by every listing, so the PIN never reaches the viewer's cache —
	// see protocol.DirResponse.OpenURL.
	OpenID string `json:"open_id,omitempty"`
	// Push is a phone-alert request from an owner's browser (see pushdir.go).
	Push *pushReq `json:"push,omitempty"`
	// ReqID names this request; the reply to it carries it back
	// (DirResponse.ReqID). Not a request of its own: a query with nothing
	// else in it is a plain listing.
	ReqID string `json:"req_id,omitempty"`
}

func (r dirQueryReq) empty() bool {
	return strings.TrimSpace(r.Pattern) == "" &&
		strings.TrimSpace(r.Transcript) == "" &&
		strings.TrimSpace(r.KeysID) == "" &&
		strings.TrimSpace(r.Keys) == "" &&
		strings.TrimSpace(r.OpenID) == "" &&
		r.Push == nil &&
		!r.Restart
}

// dialDirectoryOwner connects to a machine's directory channel and proves
// ownership (the same own_init the PIN-free connect sends, transcript-bound to
// the directory id), returning the live connection and the established
// encryption box. The caller owns the connection and MUST Close it. Shared by
// queryDirectory (list/search), SpawnOnMachine (new_session), and KillOnMachine
// (dir_kill).
// dirConn is a machine-channel connection: the socket, and once the owner
// handshake is done, its session key and sealed-frame state. writeDir and
// readDir seal and open through it.
type dirConn struct {
	*websocket.Conn
	box  *crypto.Box
	seal viewerSeal
}

func dialDirectoryOwner(machinePub ed25519.PublicKey, timeout time.Duration) (*dirConn, *crypto.Box, error) {
	if len(machinePub) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("machine key must be %d bytes", ed25519.PublicKeySize)
	}
	deviceKey, err := loadOrCreateDeviceKey()
	if err != nil {
		return nil, nil, err
	}
	devicePub := deviceKey.Public().(ed25519.PublicKey)

	dirID := crypto.DeriveDirectoryID(machinePub)
	wsURL := config.SessionWS(dirID, string(protocol.RoleViewer))
	// Bound the DIAL by the same timeout, not just the reads: DefaultDialer's
	// 45s HandshakeTimeout would otherwise let one unreachable machine stall
	// `reminal machines` (which waits on every query) for ~45s instead of ~6s.
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = timeout
	raw, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, nil, ErrDirUnreachable
	}
	conn := &dirConn{Conn: raw}
	// The relay is untrusted, and `reminal machines` opens one of these per owned
	// machine in parallel — cap the read so a malicious relay can't OOM us with an
	// oversized frame (it just fails to that machine). Matches the relay's own cap.
	conn.SetReadLimit(maxDirMessageBytes)
	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	// Join the channel. A machine that isn't hosting → relay replies "not ready"
	// → treat as unreachable/offline.
	if err := writeDir(conn, protocol.Message{Type: protocol.TypeAuth}); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := waitAuthOK(conn); err != nil {
		conn.Close()
		return nil, nil, ErrDirUnreachable
	}

	// Prove ownership: the identical own_init the PIN-free connect sends, but the
	// transcript binds the directory id.
	exHex, exID, err := crypto.NewExID()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	eph, err := crypto.NewEphemeralKey()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	viewerEph := eph.PublicKey().Bytes()
	// Signed twice, as a session's owner handshake is: the transcript as
	// before, and the one naming sealed frames.
	sig := crypto.SignOwner(deviceKey, crypto.OwnerClientTranscript(dirID, viewerEph, devicePub))
	framesSig := crypto.SignOwner(deviceKey, crypto.OwnerClientTranscriptFor(dirID, viewerEph, devicePub, true))
	if err := writeDir(conn, protocol.Message{
		Type:      protocol.TypeOwnerInit,
		ExID:      exHex,
		Data:      base64.StdEncoding.EncodeToString(viewerEph),
		DevicePub: base64.StdEncoding.EncodeToString(devicePub),
		DeviceSig: base64.StdEncoding.EncodeToString(sig),
		FramesSig: base64.StdEncoding.EncodeToString(framesSig),
		Frames:    1,
	}); err != nil {
		conn.Close()
		return nil, nil, err
	}

	if err := readOwnerResp(conn, dirID, exHex, exID, viewerEph, devicePub, machinePub, eph); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, conn.box, nil
}

// queryDirectory is QueryDirectory with optional search / one-session dump.
func queryDirectory(machinePub ed25519.PublicKey, timeout time.Duration, req dirQueryReq) (protocol.DirResponse, error) {
	return withDirBusy(func() (protocol.DirResponse, error) { return queryDirectoryOnce(machinePub, timeout, req) })
}

func queryDirectoryOnce(machinePub ed25519.PublicKey, timeout time.Duration, req dirQueryReq) (protocol.DirResponse, error) {
	var zero protocol.DirResponse
	conn, box, err := dialDirectoryOwner(machinePub, timeout)
	if err != nil {
		return zero, err
	}
	defer conn.Close()

	// Ask, and read the encrypted session list. Search / dump ride as
	// encrypted Data so older hosts (which ignore Data) still answer.
	qmsg := protocol.Message{Type: protocol.TypeDirQuery}
	req.Pattern = strings.TrimSpace(req.Pattern)
	req.Transcript = strings.TrimSpace(req.Transcript)
	req.KeysID = strings.TrimSpace(req.KeysID)
	req.Keys = strings.TrimSpace(req.Keys)
	if !req.empty() {
		reqID, _, err := crypto.NewExID()
		if err != nil {
			return zero, err
		}
		req.ReqID = reqID
		raw, err := json.Marshal(req)
		if err != nil {
			return zero, err
		}
		enc, err := box.Encrypt(raw)
		if err != nil {
			return zero, err
		}
		qmsg.Data = enc
	}
	if err := writeDir(conn, qmsg); err != nil {
		return zero, err
	}
	// Every owner on the channel hears every reply: wait for this one's.
	// The read deadline set at the dial bounds the wait.
	var maybe *protocol.DirResponse
	for {
		var msg protocol.Message
		if err := readDir(conn, &msg); err != nil {
			if maybe != nil {
				// An older machine that only ever answered with what it has:
				// what it said is the answer (the caller says what it lacks).
				return *maybe, nil
			}
			return zero, err
		}
		if msg.Type != protocol.TypeDirResp {
			continue
		}
		plain, err := box.Decrypt(msg.Data)
		if err != nil {
			return zero, fmt.Errorf("directory: decrypt failed")
		}
		var resp protocol.DirResponse
		if err := json.Unmarshal(plain, &resp); err != nil {
			return zero, fmt.Errorf("directory: bad response")
		}
		switch dirReplyFor(req, resp) {
		case dirReplyMine:
			if resp.RetryMS > 0 {
				return zero, busyAfter(resp.RetryMS)
			}
			return resp, nil
		case dirReplyMaybe:
			maybe = &resp
		}
	}
}

// dirReplyVerdict is what a reply heard on the channel is to a request.
type dirReplyVerdict int

const (
	dirReplyOthers dirReplyVerdict = iota // another owner's
	dirReplyMine                          // the answer to it
	dirReplyMaybe                         // an untagged reply that does not answer it
)

func (v dirReplyVerdict) String() string {
	return [...]string{"others", "mine", "maybe"}[v]
}

// dirReplyFor says whose a reply is. A plain listing takes any reply: each
// carries the whole listing. A request takes the reply tagged with its id;
// from a machine that tags its replies, any other is another owner's. A
// machine too old to tag them is judged by what the reply holds: one that
// answers what was asked is taken (a search cannot be told apart, and is
// taken as before); one that does not is kept, as all that machine may ever
// say, in case nothing better comes before the deadline.
func dirReplyFor(req dirQueryReq, resp protocol.DirResponse) dirReplyVerdict {
	if req.ReqID == "" {
		return dirReplyMine
	}
	if resp.Tagged || resp.ReqID != "" {
		if resp.ReqID == req.ReqID {
			return dirReplyMine
		}
		return dirReplyOthers
	}
	answered := true
	if req.KeysID != "" && req.Keys != "" {
		answered = answered && (resp.KeysOK || resp.KeysError != "")
	}
	if req.Transcript != "" {
		answered = answered && transcriptAnswered(resp, req.Transcript)
	}
	if req.Restart {
		answered = answered && (resp.RestartOK || resp.RestartError != "")
	}
	if req.OpenID != "" {
		answered = answered && (resp.OpenURL != "" || resp.OpenError != "")
	}
	if req.Push != nil {
		answered = answered && resp.Push != nil
	}
	if answered {
		return dirReplyMine
	}
	return dirReplyMaybe
}

// transcriptAnswered: the session's dump is there, or the session is not
// (the caller says it was not found).
func transcriptAnswered(resp protocol.DirResponse, id string) bool {
	for _, s := range resp.Sessions {
		if strings.EqualFold(s.ID, id) {
			return s.TranscriptOK || s.Transcript != ""
		}
	}
	return true
}

// HostTooOldError is a machine that took a request and did nothing with it:
// a reminal from before that request was known. Version is what the machine
// said it runs, when it said.
type HostTooOldError struct {
	What, Version string
}

func (e *HostTooOldError) Error() string {
	who := "its reminal"
	if e.Version != "" {
		who += " (" + e.Version + ")"
	}
	return who + " is too old to " + e.What + " — upgrade it"
}

func hostTooOld(what string, resp protocol.DirResponse) error {
	e := &HostTooOldError{What: what}
	if resp.Stats != nil {
		e.Version = resp.Stats.Version
	}
	return e
}

// SpawnOnMachine asks an owned machine to start a fresh detached session over
// its directory channel — the CLI counterpart to the web Machines panel's
// "+ New session on this host" — and returns the new session's credentials.
func SpawnOnMachine(machinePub ed25519.PublicKey, name, cwd string, timeout time.Duration) (*SpawnedSession, error) {
	return withDirBusy(func() (*SpawnedSession, error) { return spawnOnMachineOnce(machinePub, name, cwd, timeout) })
}

func spawnOnMachineOnce(machinePub ed25519.PublicKey, name, cwd string, timeout time.Duration) (*SpawnedSession, error) {
	conn, box, err := dialDirectoryOwner(machinePub, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqID, _, err := crypto.NewExID()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Name  string `json:"name"`
		Cwd   string `json:"cwd"`
		ReqID string `json:"req_id"`
	}{name, cwd, reqID})
	if err != nil {
		return nil, err
	}
	enc, err := box.Encrypt(body)
	if err != nil {
		return nil, err
	}
	if err := writeDir(conn, protocol.Message{Type: protocol.TypeNewSession, Data: enc}); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		var msg protocol.Message
		if err := readDir(conn, &msg); err != nil {
			return nil, err
		}
		if msg.Type != protocol.TypeNewSession {
			continue
		}
		plain, err := box.Decrypt(msg.Data)
		if err != nil {
			return nil, fmt.Errorf("directory: decrypt failed")
		}
		var resp struct {
			ReqID   string `json:"req_id"`
			ID      string `json:"id"`
			PIN     string `json:"pin"`
			Error   string `json:"error"`
			RetryMS int    `json:"retry_ms"`
		}
		if err := json.Unmarshal(plain, &resp); err != nil {
			return nil, fmt.Errorf("directory: bad response")
		}
		if resp.ReqID != "" && resp.ReqID != reqID {
			continue // a stale reply to some other request on this channel
		}
		if resp.RetryMS > 0 {
			return nil, busyAfter(resp.RetryMS)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		if resp.ID == "" {
			return nil, fmt.Errorf("the machine did not start a session")
		}
		return &SpawnedSession{ID: resp.ID, PIN: resp.PIN}, nil
	}
}

// KillOnMachine terminates a session on an owned machine over its directory
// channel — the CLI counterpart to the Machines panel's kill button.
func KillOnMachine(machinePub ed25519.PublicKey, sessionID string, timeout time.Duration) error {
	_, err := withDirBusy(func() (struct{}, error) { return struct{}{}, killOnMachineOnce(machinePub, sessionID, timeout) })
	return err
}

func killOnMachineOnce(machinePub ed25519.PublicKey, sessionID string, timeout time.Duration) error {
	conn, box, err := dialDirectoryOwner(machinePub, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	reqID, _, err := crypto.NewExID()
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		ID    string `json:"id"`
		ReqID string `json:"req_id"`
	}{sessionID, reqID})
	if err != nil {
		return err
	}
	enc, err := box.Encrypt(body)
	if err != nil {
		return err
	}
	if err := writeDir(conn, protocol.Message{Type: protocol.TypeDirKill, Data: enc}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		var msg protocol.Message
		if err := readDir(conn, &msg); err != nil {
			return err
		}
		if msg.Type != protocol.TypeDirKill {
			continue
		}
		plain, err := box.Decrypt(msg.Data)
		if err != nil {
			return fmt.Errorf("directory: decrypt failed")
		}
		var ack struct {
			ReqID   string `json:"req_id"`
			OK      bool   `json:"ok"`
			Error   string `json:"error"`
			RetryMS int    `json:"retry_ms"`
		}
		if err := json.Unmarshal(plain, &ack); err != nil {
			return fmt.Errorf("directory: bad response")
		}
		if ack.ReqID != "" && ack.ReqID != reqID {
			continue
		}
		if ack.RetryMS > 0 {
			return busyAfter(ack.RetryMS)
		}
		if ack.Error != "" {
			return fmt.Errorf("%s", ack.Error)
		}
		if !ack.OK {
			return fmt.Errorf("the machine did not confirm the kill")
		}
		return nil
	}
}

// readOwnerResp waits for the machine's own_resp, verifies its signature against
// the machine key we already know (stronger than the connect path's trust-on-
// first-use — here an impostor signing with any other key simply fails), and
// returns the established encryption box.
func readOwnerResp(conn *dirConn, dirID, exHex string, exID, viewerEph, devicePub, machinePub []byte, eph *ecdh.PrivateKey) error {
	for {
		var msg protocol.Message
		if err := readDir(conn, &msg); err != nil {
			return err
		}
		// The host BROADCASTS own_resp to every viewer on the channel, so when
		// more than one owner queries the same machine at once we'll also see the
		// others' replies. Skip any that isn't the answer to OUR own_init — using
		// someone else's ephemeral/signature would fail verification and wrongly
		// error out a legitimate query.
		// Over the machine's allowance for handshakes: it says when to come back.
		if msg.Type == protocol.TypeOwnerBusy && msg.ExID == exHex {
			return busyAfter(msg.RetryMS)
		}
		if msg.Type != protocol.TypeOwnerResp || msg.ExID != exHex {
			continue
		}
		agentEph, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil || len(agentEph) != crypto.PubKeyBytes {
			return fmt.Errorf("directory: bad agent key")
		}
		machineSig, err := base64.StdEncoding.DecodeString(msg.MachineSig)
		if err != nil {
			return fmt.Errorf("directory: bad machine signature")
		}
		// Verify against the machine key we already hold. Any other signer fails.
		bound, ok := ownerAnswerVerifies(dirID, viewerEph, agentEph, devicePub, machinePub, machineSig, msg.Seal)
		if !ok {
			return fmt.Errorf("directory: machine signature invalid — refusing")
		}
		// The self-reported key must also be the one we expect (defence in depth;
		// the signature check above already binds it).
		if rep, err := base64.StdEncoding.DecodeString(msg.MachinePub); err != nil || !bytes.Equal(rep, machinePub) {
			return fmt.Errorf("directory: unexpected machine identity")
		}
		peerKey, err := crypto.PeerPublicKey(agentEph)
		if err != nil {
			return fmt.Errorf("directory: invalid agent key")
		}
		shared, err := eph.ECDH(peerKey)
		if err != nil {
			return fmt.Errorf("directory: ecdh: %w", err)
		}
		wrapped, err := base64.StdEncoding.DecodeString(msg.Wrap)
		if err != nil {
			return fmt.Errorf("directory: bad wrap")
		}
		sessionKey, err := crypto.UnwrapSessionKey(shared, exID, wrapped)
		if err != nil {
			return fmt.Errorf("directory: session key unwrap failed")
		}
		box, err := crypto.NewBox(sessionKey)
		if err != nil {
			return err
		}
		seal, err := admitSeal(sessionKey, shared, exID, msg.Seal, bound, "m:"+base64.StdEncoding.EncodeToString(machinePub))
		if err != nil {
			return err
		}
		conn.box, conn.seal = box, seal
		return nil
	}
}

func writeDir(conn *dirConn, msg protocol.Message) error {
	data, err := conn.seal.out(conn.box, msg)
	if err != nil {
		return err
	}
	if data == nil {
		if data, err = json.Marshal(msg); err != nil {
			return err
		}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func readDir(conn *dirConn, out *protocol.Message) error {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg protocol.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if conn.box == nil {
			// Before our handshake: the channel is shared, so other owners'
			// sealed traffic goes by; none of it is ours to read.
			if msg.Type == protocol.TypeSealed {
				continue
			}
		} else {
			m, ok, err := conn.seal.in(conn.box, msg)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			msg = m
		}
		*out = msg
		if out.Type == protocol.TypeError {
			return fmt.Errorf("%s", out.Error)
		}
		return nil
	}
}

func waitAuthOK(conn *dirConn) error {
	for {
		var msg protocol.Message
		if err := readDir(conn, &msg); err != nil {
			return err
		}
		if msg.Type == protocol.TypeAuthOK {
			return nil
		}
	}
}

// parseDirQuery decrypts an optional TypeDirQuery payload. Empty / garbage
// returns a zero req so the host still answers with a plain session list.
func parseDirQuery(box *crypto.Box, data string) dirQueryReq {
	if box == nil || data == "" {
		return dirQueryReq{}
	}
	plain, err := box.Decrypt(data)
	if err != nil {
		return dirQueryReq{}
	}
	var req dirQueryReq
	if json.Unmarshal(plain, &req) != nil {
		return dirQueryReq{}
	}
	req.Pattern = strings.TrimSpace(req.Pattern)
	req.Transcript = strings.TrimSpace(req.Transcript)
	req.KeysID = strings.TrimSpace(req.KeysID)
	req.Keys = strings.TrimSpace(req.Keys)
	if !reqIDShape.MatchString(req.ReqID) {
		req.ReqID = "" // repeated back to every owner: an id, nothing else
	}
	return req
}

// reqIDShape is a request id as callers make them: hex (crypto.NewExID) or
// base 36 (the web viewer).
var reqIDShape = regexp.MustCompile(`^[0-9a-z]{1,64}$`)
