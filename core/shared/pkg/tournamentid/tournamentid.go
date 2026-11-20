package tournamentid

import (
	"encoding/binary"

	"github.com/google/uuid"
)

var sessionNamespace = uuid.MustParse("3b8f6a52-41d7-4c1e-9a0b-5d2f7e6c9184")

func Session(tournament uuid.UUID, round, board int) uuid.UUID {
	name := make([]byte, 0, len(tournament)+8)
	name = append(name, tournament[:]...)
	name = binary.BigEndian.AppendUint32(name, uint32(round))
	name = binary.BigEndian.AppendUint32(name, uint32(board))
	return uuid.NewSHA1(sessionNamespace, name)
}
