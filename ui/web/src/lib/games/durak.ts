export const ranks = "6789TJQKA";
export const suits = "SCDH";
export const handSize = 6;

export enum Suit {
  Spades = "S",
  Clubs = "C",
  Diamonds = "D",
  Hearts = "H",
}

export const suitSymbols: Record<Suit, string> = { [Suit.Spades]: "♠", [Suit.Clubs]: "♣", [Suit.Diamonds]: "♦", [Suit.Hearts]: "♥" };
export const suitNames: Record<Suit, string> = { [Suit.Spades]: "spades", [Suit.Clubs]: "clubs", [Suit.Diamonds]: "diamonds", [Suit.Hearts]: "hearts" };
const rankNames: Record<string, string> = { T: "10", J: "jack", Q: "queen", K: "king", A: "ace" };

export type TablePair = { attack: string; defense?: string };

export type DurakHandView = { cards: string[]; count: number; hidden: boolean };

export type DurakView = {
  hands: DurakHandView[];
  trump: string;
  stock: number;
  table: TablePair[];
  discard: number;
  attackerSeat: number;
  taking: boolean;
  boutLimit: number;
};

export type DurakPosition = {
  hands: [string[], string[]];
  stock: string[];
  trump: string;
  table: TablePair[];
  discard: string[];
  attacker: 0 | 1;
  limit: number;
  taking: boolean;
  moves: string[];
};

export const rankOf = (card: string) => ranks.indexOf(card[0] ?? "");
export const suitOf = (card: string) => (card[1] ?? "") as Suit;
export const isCard = (card: string) => card.length === 2 && rankOf(card) >= 0 && suits.includes(card[1] ?? "-");
export const rankLabel = (card: string) => card[0] === "T" ? "10" : card[0] ?? "";
export const cardName = (card: string) => `${rankNames[card[0] ?? ""] ?? card[0]} of ${suitNames[suitOf(card)] ?? "?"}`;
export const isRed = (card: string) => suitOf(card) === Suit.Hearts || suitOf(card) === Suit.Diamonds;

export function beats(defense: string, attack: string, trump: Suit) {
  if (suitOf(defense) === suitOf(attack)) return rankOf(defense) > rankOf(attack);
  return suitOf(defense) === trump;
}

export function sortHand(hand: string[], trump: Suit) {
  const weight = (card: string) => (suitOf(card) === trump ? 4 : suits.indexOf(suitOf(card))) * 16 + rankOf(card);
  return [...hand].sort((a, b) => weight(a) - weight(b));
}

export function deck() {
  return [...suits].flatMap(suit => [...ranks].map(rank => rank + suit));
}

export function deal(random: () => number = Math.random): DurakPosition {
  const cards = deck();
  for (let i = cards.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [cards[i], cards[j]] = [cards[j]!, cards[i]!];
  }
  return fromDeck(cards);
}

export function fromDeck(cards: string[]): DurakPosition {
  const trump = cards[2 * handSize]!;
  const suit = suitOf(trump);
  const hands: [string[], string[]] = [sortHand(cards.slice(0, handSize), suit), sortHand(cards.slice(handSize, 2 * handSize), suit)];
  const attacker = firstAttacker(hands, suit);
  return { hands, stock: [...cards.slice(2 * handSize + 1), trump], trump, table: [], discard: [], attacker, limit: Math.min(handSize, hands[1 - attacker]!.length), taking: false, moves: [] };
}

export function firstAttacker(hands: string[][], trump: Suit): 0 | 1 {
  let seat: 0 | 1 = 0;
  let lowest = ranks.length;
  hands.forEach((hand, at) => {
    for (const card of hand) {
      if (suitOf(card) === trump && rankOf(card) < lowest) {
        lowest = rankOf(card);
        seat = at === 1 ? 1 : 0;
      }
    }
  });
  return seat;
}

const defender = (p: DurakPosition): 0 | 1 => p.attacker === 0 ? 1 : 0;
const openPair = (p: { table: TablePair[] }) => p.table.findIndex(pair => !pair.defense);

export function turn(p: DurakPosition): 0 | 1 {
  return !p.taking && openPair(p) >= 0 ? defender(p) : p.attacker;
}

function onTable(p: DurakPosition, card: string) {
  return p.table.some(pair => rankOf(pair.attack) === rankOf(card) || (pair.defense !== undefined && rankOf(pair.defense) === rankOf(card)));
}

function canAttack(p: DurakPosition, card: string) {
  if (turn(p) !== p.attacker || p.table.length >= p.limit) return false;
  return p.table.length === 0 || onTable(p, card);
}

export function legalMoves(p: DurakPosition): string[] {
  if (outcome(p).over) return [];
  const seat = turn(p);
  const trump = suitOf(p.trump);
  if (seat !== p.attacker) {
    const attack = p.table[openPair(p)]!.attack;
    return [...p.hands[seat].filter(card => beats(card, attack, trump)).map(card => `defend ${attack} ${card}`), "take"];
  }
  const out = p.hands[seat].filter(card => canAttack(p, card)).map(card => `attack ${card}`);
  return p.table.length > 0 ? [...out, "pass"] : out;
}

function draw(hand: string[], stock: string[], trump: Suit) {
  const need = Math.max(0, handSize - hand.length);
  return { hand: sortHand([...hand, ...stock.slice(0, need)], trump), stock: stock.slice(need) };
}

export function play(p: DurakPosition, move: string): DurakPosition {
  if (!legalMoves(p).includes(move)) throw new Error(`illegal durak move ${move}`);
  const seat = turn(p);
  const [action, first, second] = move.split(" ");
  const hands: [string[], string[]] = [[...p.hands[0]], [...p.hands[1]]];
  const next: DurakPosition = { ...p, hands, table: p.table.map(pair => ({ ...pair })), moves: [...p.moves, move] };
  if (action === "attack") {
    hands[seat] = hands[seat].filter(card => card !== first);
    next.table.push({ attack: first! });
  } else if (action === "defend") {
    hands[seat] = hands[seat].filter(card => card !== second);
    next.table[openPair(next)]!.defense = second;
  } else if (action === "take") {
    next.taking = true;
  } else {
    const played = p.table.flatMap(pair => pair.defense ? [pair.attack, pair.defense] : [pair.attack]);
    const trump = suitOf(p.trump);
    const from = p.attacker, to = defender(p);
    if (p.taking) hands[to] = [...hands[to], ...played];
    else next.discard = [...p.discard, ...played];
    next.table = [];
    let stock = p.stock;
    for (const who of [from, to]) {
      const refill = draw(hands[who], stock, trump);
      hands[who] = refill.hand;
      stock = refill.stock;
    }
    next.stock = stock;
    next.attacker = p.taking ? from : to;
    next.taking = false;
    next.limit = Math.min(handSize, hands[defender(next)].length);
  }
  return next;
}

export function outcome(p: DurakPosition): { over: boolean; winner?: 0 | 1 } {
  if (p.stock.length > 0 || p.table.length > 0) return { over: false };
  const out = [p.hands[0].length === 0, p.hands[1].length === 0];
  if (out[0] && out[1]) return { over: true };
  if (out[0]) return { over: true, winner: 0 };
  if (out[1]) return { over: true, winner: 1 };
  return { over: false };
}

export function viewOf(p: DurakPosition, seat: number, reveal = false): DurakView {
  return {
    hands: p.hands.map((cards, at) => ({ cards: reveal || at === seat ? cards : [], count: cards.length, hidden: !reveal && at !== seat })),
    trump: p.trump, stock: p.stock.length, table: p.table, discard: p.discard.length, attackerSeat: p.attacker, taking: p.taking, boutLimit: p.limit,
  };
}

export function openAttack(view: { table: TablePair[] }) {
  const at = openPair(view);
  return at < 0 ? undefined : view.table[at]!.attack;
}

export function cardMove(card: string, legal: string[]) {
  return legal.find(move => move === `attack ${card}` || (move.startsWith("defend ") && move.endsWith(` ${card}`)));
}

export function describeMove(move: string) {
  const [action, first, second] = move.split(" ");
  if (action === "attack" && first) return `attacks with the ${cardName(first)}`;
  if (action === "defend" && first && second) return `beats the ${cardName(first)} with the ${cardName(second)}`;
  if (action === "take") return "takes the cards";
  if (action === "pass") return "passes";
  return move;
}
