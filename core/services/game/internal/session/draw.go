package session

type DrawAction int

const (
	OfferDraw DrawAction = iota + 1
	AcceptDraw
	DeclineDraw
)
