import { describe, expect, it } from "vitest";

import { botMove } from "@/lib/games/durak-bot";
import { beats, cardMove, deal, deck, firstAttacker, fromDeck, isCard, legalMoves, outcome, play, Suit, turn, viewOf, type DurakPosition } from "@/lib/games/durak";

const cards = (raw: string) => raw ? raw.split(" ") : [];

function bout(attacker: string, defender: string, stock = "", limit = 6, trump = "6S"): DurakPosition {
  return { hands: [cards(attacker), cards(defender)], stock: cards(stock), trump, table: [], discard: [], attacker: 0, limit, taking: false, moves: [] };
}

const run = (position: DurakPosition, ...moves: string[]) => moves.reduce(play, position);

function seeded(seed: number) {
  let state = seed;
  return () => {
    state = (state * 1103515245 + 12345) % 2147483648;
    return state / 2147483648;
  };
}

describe("durak rules", () => {
  it.each([
    ["KH", "7H", true], ["7H", "KH", false], ["6S", "AH", true], ["AH", "6S", false], ["7S", "6S", true], ["6S", "7S", false], ["AC", "6D", false],
  ] as const)("%s beats %s with spades trump = %s", (defense, attack, want) => {
    expect(beats(defense, attack, Suit.Spades)).toBe(want);
  });

  it("deals six each with the trump under the stock", () => {
    const p = deal(seeded(3));
    expect(p.hands[0]).toHaveLength(6);
    expect(p.hands[1]).toHaveLength(6);
    expect(p.stock).toHaveLength(24);
    expect(p.stock.at(-1)).toBe(p.trump);
    expect([...p.hands[0], ...p.hands[1], ...p.stock].sort()).toEqual(deck().sort());
    expect(turn(p)).toBe(p.attacker);
  });

  it("gives the first attack to the lowest trump", () => {
    expect(firstAttacker([cards("6S 7S"), cards("9H 6C")], Suit.Hearts)).toBe(1);
    expect(firstAttacker([cards("7D 8D"), cards("6D AD")], Suit.Diamonds)).toBe(1);
    expect(firstAttacker([cards("6S 7S"), cards("8S 9S")], Suit.Hearts)).toBe(0);
    const ordered = deck();
    expect(fromDeck(ordered).trump).toBe(ordered[12]);
  });

  it("answers an attack with defences or a take", () => {
    const p = run(bout("7H 9C", "8H AH 6C 7S", "", 4), "attack 7H");
    expect(turn(p)).toBe(1);
    expect(legalMoves(p)).toEqual(["defend 7H 8H", "defend 7H AH", "defend 7H 7S", "take"]);
    expect(legalMoves(run(p, "defend 7H 8H"))).toEqual(["pass"]);
    expect(() => play(p, "defend 7H 6C")).toThrow();
  });

  it.each([
    ["rank on the table", bout("7H 7C 8D", "8H 9H TH", "", 3), ["attack 7H", "defend 7H 8H"], ["attack 7C", "attack 8D", "pass"]],
    ["defender's starting hand caps the bout", bout("7H 7C 7D", "8H 8C", "", 2), ["attack 7H", "defend 7H 8H", "attack 7C", "defend 7C 8C"], ["pass"]],
    ["throw-ins while taking", bout("7H 7C 7D", "8S 9S", "", 2), ["attack 7H", "take"], ["attack 7C", "attack 7D", "pass"]],
    ["throw-ins on a take stop at the limit", bout("7H 7C 7D", "8S 9S", "", 2), ["attack 7H", "take", "attack 7C"], ["pass"]],
  ] as const)("throw-in limits: %s", (_, position, moves, want) => {
    expect(legalMoves(run(position, ...moves))).toEqual(want);
  });

  it("discards on a pass and hands the table over on a take, refilling the attacker first", () => {
    const passed = run(bout("7H 9C", "8H AH", "6C 6D 6H TC TD JD", 2), "attack 7H", "defend 7H 8H", "pass");
    expect(passed.discard).toEqual(["7H", "8H"]);
    expect(passed.attacker).toBe(1);
    expect(passed.hands[0].join(" ")).toBe("6C 9C TC 6D TD 6H");
    expect(passed.hands[1].join(" ")).toBe("JD AH");
    const taken = run(bout("7H 7C 9D", "8S AH", "6C 6D", 2), "attack 7H", "take", "attack 7C", "pass");
    expect(taken.attacker).toBe(0);
    expect(taken.hands[1].join(" ")).toBe("7C 7H AH 8S");
    expect(taken.hands[0].join(" ")).toBe("6C 6D 9D");
    expect(taken.limit).toBe(4);
  });

  it.each([
    ["both out in one bout is a draw", "7H", "8H", ["attack 7H", "defend 7H 8H", "pass"], { over: true }],
    ["the attacker goes out on a take", "7H", "6C", ["attack 7H", "take", "pass"], { over: true, winner: 0 }],
    ["the defender goes out", "7H 9C", "8H", ["attack 7H", "defend 7H 8H", "pass"], { over: true, winner: 1 }],
  ] as const)("end: %s", (_, attacker, defender, moves, want) => {
    expect(outcome(run(bout(attacker, defender, "", cards(defender).length), ...moves))).toEqual(want);
  });

  it("matches a card to the server's legal moves", () => {
    expect(cardMove("KH", ["defend 7H KH", "take"])).toBe("defend 7H KH");
    expect(cardMove("7H", ["defend 7H KH", "take"])).toBeUndefined();
    expect(cardMove("9C", ["attack 9C", "pass"])).toBe("attack 9C");
    expect(isCard("TS")).toBe(true);
    expect(isCard("1S")).toBe(false);
  });

  it("hides the other hand in a view", () => {
    const p = deal(seeded(5));
    const view = viewOf(p, 0);
    expect(view.hands[1]).toEqual({ cards: [], count: 6, hidden: true });
    expect(view.hands[0]!.cards).toEqual(p.hands[0]);
    expect(viewOf(p, 0, true).hands[1]!.cards).toEqual(p.hands[1]);
  });
});

describe("durak bot", () => {
  it("plays legal moves until every game ends and keeps all 36 cards", () => {
    for (let seed = 1; seed <= 150; seed++) {
      let p = deal(seeded(seed));
      let plies = 0;
      while (!outcome(p).over) {
        const move = botMove(p);
        expect(move && legalMoves(p).includes(move)).toBe(true);
        p = play(p, move!);
        const all = [...p.hands[0], ...p.hands[1], ...p.stock, ...p.discard, ...p.table.flatMap(pair => pair.defense ? [pair.attack, pair.defense] : [pair.attack])];
        expect(all.sort()).toEqual(deck().sort());
        expect(++plies).toBeLessThan(1000);
      }
    }
  });

  it("beats cheaply, takes when it cannot and opens with its lowest plain card", () => {
    expect(botMove(run(bout("7H", "8H 6S 9H", "", 3), "attack 7H"))).toBe("defend 7H 8H");
    expect(botMove(run(bout("AH", "8H 6C", "", 2), "attack AH"))).toBe("take");
    expect(botMove(bout("6S KC 9D", "7H"))).toBe("attack 9D");
  });
});
