package main

import "unicode"

// maxRoomName caps a room's name at 64 UTF-8 bytes: the name is the key of the
// rooms map, taken verbatim from the client's URL, so without a bound any
// request could grow that map (and every copy of the name) without limit.
// A string's len is its UTF-8 byte length, so multibyte names are bounded the
// same way as ASCII ones.
const maxRoomName = 64

// validRoomName decides whether a requested room name is accepted, at the one
// point room names enter the server (handleWS). A room name is a key, not a
// display value: unlike aliases (alias.go) it is never rewritten, because
// silently joining a differently spelled room would split people from the ones
// they meant to meet. A name is therefore either taken as-is or refused with
// 400 Bad Request.
//
//	Refused: a name over maxRoomName bytes, and any name carrying a control
//	rune (C0 such as newline and ESC, C1 and DEL: they can't be typed by a
//	person, they break URLs, and rooms spelled with them would be unreachable
//	or ambiguous).
//
//	Kept: everything else, byte for byte, whatever the script - accents, emoji,
//	punctuation, spaces. This is not an ASCII whitelist.
func validRoomName(name string) bool {
	if len(name) > maxRoomName {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
