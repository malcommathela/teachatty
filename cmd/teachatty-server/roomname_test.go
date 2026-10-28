package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// tryRoom dials handleWS asking for the given room name (URL-escaped, the way a
// client's URL carries multibyte runes and control bytes) and returns the dial
// result as-is: a *websocket.Conn when the upgrade happened, or the error plus
// the server's HTTP response when it was refused.
func tryRoom(t *testing.T, srv *httptest.Server, name string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + url.QueryEscape(name) + "&alias=ana"
	return websocket.DefaultDialer.Dial(u, nil)
}

// refused wants the dial for this name to be refused with 400, before the
// WebSocket upgrade, and to leave no room behind under that name.
func refused(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	conn, resp, err := tryRoom(t, srv, name)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatalf("room name %q should be refused", name)
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("room name %q: expected 400, got %v (error %v)", name, resp, err)
	}
	if members(name) != -1 {
		t.Fatalf("refused name %q must not create a room", name)
	}
}

// accepted wants the dial for this name to succeed and join a room under that
// name, which is then closed again so the room vanishes.
func accepted(t *testing.T, srv *httptest.Server, name string) {
	t.Helper()
	conn, _, err := tryRoom(t, srv, name)
	if err != nil {
		t.Fatalf("room name %q was refused: %v", name, err)
	}
	eventually(t, "the member joins room "+name, func() bool { return members(name) == 1 })
	conn.Close()
	eventually(t, "room "+name+" vanishes", func() bool { return members(name) == -1 })
}

// an ordinary name is accepted and creates its room
func TestOrdinaryRoomNameIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handleWS))
	defer srv.Close()

	accepted(t, srv, "team-meeting")
}

// 64 bytes exactly is allowed; one byte more is refused
func TestRoomNameByteLimitIsInclusive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handleWS))
	defer srv.Close()

	accepted(t, srv, strings.Repeat("a", maxRoomName))
	refused(t, srv, strings.Repeat("a", maxRoomName+1))
}

// multibyte names are measured in UTF-8 bytes, not runes: 32 two-byte runes
// fill the limit exactly, one more rune breaks it
func TestRoomNameLimitIsBytesNotRunes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handleWS))
	defer srv.Close()

	accepted(t, srv, strings.Repeat("é", maxRoomName/2))  // 64 bytes, 32 runes
	refused(t, srv, strings.Repeat("é", maxRoomName/2+1)) // 66 bytes, 33 runes
}

// control runes are refused, though the rest of the name is valid Unicode
func TestRoomNameWithControlRunesIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handleWS))
	defer srv.Close()

	for _, name := range []string{
		"a\nb",   // newline
		"a\rb",   // carriage return
		"a\x1bb", // ESC
		"a\x00b", // NUL
	} {
		refused(t, srv, name)
	}
}

// validRoomName itself, including names a URL can't carry raw (unit level, so
// the handler tests above don't have to spell out every control rune)
func TestValidRoomName(t *testing.T) {
	for name, want := range map[string]bool{
		"local":                              true,
		"team-meeting":                       true,
		"café ☕":                             true, // accents, emoji and spaces survive
		strings.Repeat("a", maxRoomName):     true,
		strings.Repeat("é", maxRoomName/2):   true,
		strings.Repeat("a", maxRoomName+1):   false,
		strings.Repeat("é", maxRoomName/2+1): false,
		"a\nb":                               false,
		"a\x1bb":                             false,
		"a\x7fb":                             false, // DEL
		"":                                   true,  // never judged as such: empty becomes defaultRoom first
	} {
		if got := validRoomName(name); got != want {
			t.Errorf("validRoomName(%q) = %v, want %v", name, got, want)
		}
	}
}

// an omitted room parameter is the default room; an empty one (present in the
// URL, no value) is the same
func TestOmittedAndEmptyRoomDefaultToLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(handleWS))
	defer srv.Close()

	ana, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?alias=ana", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ana.Close()
	eventually(t, "ana (no room parameter) joins local", func() bool { return members(defaultRoom) == 1 })

	bia, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?room=&alias=bia", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bia.Close()
	eventually(t, "bia (empty room parameter) joins local", func() bool { return members(defaultRoom) == 2 })

	// they can talk to each other: one room, not two look-alikes
	if err := ana.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	bia.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := bia.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "ana: hi" {
		t.Fatalf("bia got %q, want %q", msg, "ana: hi")
	}
}
