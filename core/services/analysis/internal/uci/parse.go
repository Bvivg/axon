package uci

import (
	"regexp"
	"strconv"
	"strings"
)

type Score struct {
	Mate  bool
	Value int
}

type Info struct {
	Depth    int
	MultiPV  int
	Score    Score
	HasScore bool
	Bound    bool
	PV       []string
}

var moveToken = regexp.MustCompile(`^[a-h][1-8][a-h][1-8][qrbn]?$`)

func ValidMove(move string) bool { return moveToken.MatchString(move) }

func ParseInfo(line string) (Info, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "info" || fields[1] == "string" {
		return Info{}, false
	}
	var info Info
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "depth":
			info.Depth = number(fields, i+1)
			i++
		case "multipv":
			info.MultiPV = number(fields, i+1)
			i++
		case "score":
			if i+2 >= len(fields) {
				return Info{}, false
			}
			value, err := strconv.Atoi(fields[i+2])
			if err != nil {
				return Info{}, false
			}
			switch fields[i+1] {
			case "cp":
				info.Score = Score{Value: value}
			case "mate":
				info.Score = Score{Mate: true, Value: value}
			default:
				return Info{}, false
			}
			info.HasScore = true
			i += 2
			if i+1 < len(fields) && (fields[i+1] == "lowerbound" || fields[i+1] == "upperbound") {
				info.Bound = true
				i++
			}
		case "pv":
			info.PV = append([]string(nil), fields[i+1:]...)
			i = len(fields)
		case "string":
			i = len(fields)
		}
	}
	return info, true
}

func number(fields []string, at int) int {
	if at >= len(fields) {
		return 0
	}
	value, err := strconv.Atoi(fields[at])
	if err != nil {
		return 0
	}
	return value
}

func ParseBestMove(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "bestmove" {
		return "", false
	}
	if fields[1] == "(none)" || fields[1] == "0000" {
		return "", true
	}
	return fields[1], true
}

func ParseName(line string) (string, bool) {
	name, ok := strings.CutPrefix(line, "id name ")
	return strings.TrimSpace(name), ok
}
