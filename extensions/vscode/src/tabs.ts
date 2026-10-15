// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// TabOrder says which terminal a tab in the terminal tab list draws. The tab
// list asks decoration providers about each tab by a URI that carries the
// workbench's instance id for that terminal, and the API never says which
// vscode.Terminal an id is. What does line up is order: instance ids are handed
// out in the order terminals are created in a window, and window.terminals
// lists them in that same order. So once every tab has been seen, the k-th
// smallest live id is the k-th terminal. While they haven't all been seen (one
// not drawn yet, one of another kind) nothing is said rather than a guess.
//
// Kept free of the vscode module so it can be tested with plain node.

export class TabOrder<U> {
  private live = new Map<number, U>(); // instance id -> its URI
  // Ids of closed terminals: the decoration service keeps asking about every
  // URI it has seen, so a closed terminal's comes back and must not count.
  private dead = new Set<number>();
  // A tab was asked about before every tab had been seen and got no answer.
  // The tab list does not ask again on its own.
  private missed = false;
  // Closes that couldn't be tied to an id yet, as the place each one had in
  // the list of terminals there were when it closed, oldest first.
  private unplaced: number[] = [];

  // ask records a tab's id and returns its place among the `count` terminals
  // that have tabs, or undefined when that can't be told for sure. `again` is
  // true the first time an answer is possible after some tab got none: every
  // tab should then be asked again.
  ask(id: number, uri: U, count: number): { index?: number; again: boolean } {
    if (this.dead.has(id)) return { again: false };
    this.live.set(id, uri);
    // The closed ones' URIs are asked about again too. Once the ids number
    // the terminals there were before the first unplaced close, they are
    // exactly those, in that order (a newer terminal only adds a larger id
    // at the end), and each close's place says which id was its.
    if (this.unplaced.length && this.live.size === count + this.unplaced.length) {
      const ids = this.ids();
      for (const k of this.unplaced) {
        const [gone] = ids.splice(k, 1);
        this.dead.add(gone);
        this.live.delete(gone);
      }
      this.unplaced = [];
    }
    if (this.unplaced.length || this.live.size !== count) {
      this.missed = true;
      return { again: false };
    }
    const again = this.missed;
    this.missed = false;
    return { index: this.ids().indexOf(id), again };
  }

  // uriAt is the URI of the k-th of `count` terminals, when it is known.
  uriAt(k: number, count: number): U | undefined {
    if (k < 0 || this.unplaced.length || this.live.size !== count) return undefined;
    return this.live.get(this.ids()[k]);
  }

  // closed is told that the k-th of the `count` terminals there were has
  // closed (k is -1 when it was not one of them). A closed terminal is already
  // gone from window.terminals by then, so its place in the list as it was is
  // what says which id was its. A tab not seen yet is one opened since, at the
  // end of the list. So when not every tab has been seen and the closed one
  // is among the seen ones, its id is told once the unseen tabs are (see
  // ask). Otherwise every id is learnt again from the tabs as they are next
  // drawn; the ones known dead stay dead.
  closed(k: number, count: number): void {
    const ids = this.ids();
    if (k >= 0 && !this.unplaced.length && ids.length === count) {
      this.dead.add(ids[k]);
      this.live.delete(ids[k]);
    } else if (k >= 0 && k < ids.length) {
      this.unplaced.push(k);
      this.missed = true;
    } else {
      this.live.clear();
      this.missed = true;
    }
  }

  private ids(): number[] {
    return [...this.live.keys()].sort((a, b) => a - b);
  }
}
