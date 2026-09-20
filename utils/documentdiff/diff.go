// Package documentdiff produces a compact, line-oriented diff suitable for
// version review. It purposefully bounds algorithmic work for API safety.
package documentdiff

import "strings"

type Kind string

const (
	Unchanged Kind = "unchanged"
	Added     Kind = "added"
	Removed   Kind = "removed"
)

type Segment struct {
	Kind Kind   `json:"kind"`
	Text string `json:"text"`
}

const maxLines = 700

func Compare(before, after string) []Segment {
	left, right := lines(before), lines(after)
	if len(left) > maxLines || len(right) > maxLines {
		return []Segment{{Kind: Removed, Text: before}, {Kind: Added, Text: after}}
	}
	table := make([][]int, len(left)+1)
	for index := range table {
		table[index] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	segments := make([]Segment, 0, len(left)+len(right))
	appendSegment := func(kind Kind, text string) {
		if len(segments) > 0 && segments[len(segments)-1].Kind == kind {
			segments[len(segments)-1].Text += "\n" + text
			return
		}
		segments = append(segments, Segment{Kind: kind, Text: text})
	}
	for i, j := 0, 0; i < len(left) || j < len(right); {
		switch {
		case i == len(left):
			appendSegment(Added, right[j])
			j++
		case j == len(right):
			appendSegment(Removed, left[i])
			i++
		case left[i] == right[j]:
			appendSegment(Unchanged, left[i])
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			appendSegment(Removed, left[i])
			i++
		default:
			appendSegment(Added, right[j])
			j++
		}
	}
	return segments
}

func lines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}
