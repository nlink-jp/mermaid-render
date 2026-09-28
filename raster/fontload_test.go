package raster

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

type nameRec struct {
	platform, encoding, language, id uint16
	raw                              []byte
}

func utf16be(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.BigEndian.AppendUint16(b, u)
	}
	return b
}

// nameTable builds a name table (format 0).
func nameTable(recs []nameRec) []byte {
	var strs []byte
	t := binary.BigEndian.AppendUint16(nil, 0)
	t = binary.BigEndian.AppendUint16(t, uint16(len(recs)))
	t = binary.BigEndian.AppendUint16(t, uint16(6+12*len(recs)))
	for _, r := range recs {
		for _, v := range []uint16{r.platform, r.encoding, r.language, r.id, uint16(len(r.raw)), uint16(len(strs))} {
			t = binary.BigEndian.AppendUint16(t, v)
		}
		strs = append(strs, r.raw...)
	}
	return append(t, strs...)
}

// collection builds a font collection whose faces hold only a name table:
// enough for faceNames, not for sfnt, so choosing a face also exercises
// the per-face load error.
func collection(faces [][]nameRec) []byte {
	head := 12 + 4*len(faces)
	dirLen := 12 + 16
	var tables [][]byte
	for _, f := range faces {
		tables = append(tables, nameTable(f))
	}
	b := []byte("ttcf")
	b = binary.BigEndian.AppendUint32(b, 0x00010000)
	b = binary.BigEndian.AppendUint32(b, uint32(len(faces)))
	off := head
	for range faces {
		b = binary.BigEndian.AppendUint32(b, uint32(off))
		off += dirLen
	}
	tableAt := off
	for i := range faces {
		b = binary.BigEndian.AppendUint32(b, 0x00010000)
		b = binary.BigEndian.AppendUint16(b, 1)
		b = append(b, make([]byte, 6)...)
		b = append(b, "name"...)
		b = binary.BigEndian.AppendUint32(b, 0)
		b = binary.BigEndian.AppendUint32(b, uint32(tableAt))
		b = binary.BigEndian.AppendUint32(b, uint32(len(tables[i])))
		tableAt += len(tables[i])
	}
	for _, t := range tables {
		b = append(b, t...)
	}
	return b
}

var twoFaces = [][]nameRec{
	{
		{3, 1, 0x409, 4, utf16be("Test Sans Regular")},
		{3, 1, 0x409, 6, utf16be("TestSans-Regular")},
	},
	{
		{3, 1, 0x409, 4, utf16be("Test Sans Bold")},
		{3, 1, 0x411, 4, utf16be("テスト 太字")},
		{1, 0, 0, 4, []byte("Caf\x8e Bold")},
		{3, 1, 0x409, 6, utf16be("TestSans-Bold")},
		{1, 1, 11, 4, []byte{0x83, 0x65}}, // Macintosh Japanese: skipped
	},
}

func TestFaceNames(t *testing.T) {
	b := collection(twoFaces)
	offs, err := faceOffsets(b)
	if err != nil || len(offs) != 2 {
		t.Fatalf("faceOffsets: %v %v", offs, err)
	}
	names, err := faceNames(b, offs[1])
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range names {
		got = append(got, n.text)
	}
	want := "Test Sans Bold|テスト 太字|Café Bold|TestSans-Bold"
	if strings.Join(got, "|") != want {
		t.Errorf("names %q, want %q", strings.Join(got, "|"), want)
	}
}

func TestOpenFaceByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ttc")
	if err := os.WriteFile(path, collection(twoFaces), 0o644); err != nil {
		t.Fatal(err)
	}
	// Every name of the second face, in any language and case, finds it;
	// the face then fails to load (it has no glyphs), and the error names
	// the face, not just the file.
	for _, name := range []string{"TestSans-Bold", "testsans-bold", "テスト 太字", "Café Bold", "TEST SANS BOLD"} {
		_, err := openFace(path, name)
		if err == nil || !strings.Contains(err.Error(), `"TestSans-Bold"`) {
			t.Errorf("%q: %v, want the second face's load error", name, err)
		}
	}
	_, err := openFace(path, "Nope")
	if err == nil || !strings.Contains(err.Error(), `"TestSans-Regular"`) || !strings.Contains(err.Error(), `"TestSans-Bold"`) {
		t.Errorf("a missing name: %v, want the faces listed", err)
	}
}

func TestFaceNamesTruncated(t *testing.T) {
	b := collection(twoFaces)
	for n := 0; n < len(b); n++ {
		offs, err := faceOffsets(b[:n])
		if err != nil {
			continue
		}
		for _, o := range offs {
			faceNames(b[:n], o) // must not panic
		}
	}
}

const (
	menlo = "/System/Library/Fonts/Menlo.ttc"
)

func needFont(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no %s", path)
	}
	if _, err := os.Stat(hiraginoW3); err != nil {
		t.Skip("no Hiragino")
	}
}

func TestLoadFontSystem(t *testing.T) {
	needFont(t, menlo)
	for _, spec := range []FontSpec{
		{Path: menlo},
		{Path: menlo, Name: "Menlo-Bold"},
		{Path: menlo, Name: "menlo bold"},
		{Path: menlo, Name: "Menlo-Regular", BoldPath: menlo, BoldName: "Menlo-Bold"},
		{Path: hiraginoW6, Name: "ヒラギノ角ゴシック W6"},
		{Path: hiraginoW6, Name: "HiraginoSans-W6"},
	} {
		if _, err := LoadFont(spec); err != nil {
			t.Errorf("%+v: %v", spec, err)
		}
	}
	if _, err := LoadFont(FontSpec{Path: menlo, Name: "Helvetica"}); err == nil || !strings.Contains(err.Error(), "Menlo-Regular") {
		t.Errorf("a name the file lacks: %v, want the faces listed", err)
	}
	if _, err := LoadFont(FontSpec{Path: "/nonexistent.ttf"}); err == nil {
		t.Error("a missing file loaded")
	}
	if _, err := LoadFont(FontSpec{}); err == nil {
		t.Error("an empty spec loaded")
	}
}

// Characters the chosen face lacks come from Hiragino one by one; a
// character no face has is an error, not a gap.
func TestLoadFontFallback(t *testing.T) {
	needFont(t, menlo)
	fn, err := LoadFont(FontSpec{Path: menlo})
	if err != nil {
		t.Fatal(err)
	}
	rs, err := fn.runs("ID 日本", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].f == rs[1].f || rs[0].s != "ID " || rs[1].s != "日本" {
		t.Errorf("runs %+v, want Menlo then Hiragino", rs)
	}
	_, _, err = fn.measureEm("ok 😀", false)
	var me *MissingGlyphError
	if !errors.As(err, &me) || me.Rune != '😀' {
		t.Errorf("an emoji: %v, want a MissingGlyphError", err)
	}
	if _, err := RenderSource("flowchart LR\n A[設定] --> B[config]", Options{Font: fn}); err != nil {
		t.Errorf("render with Menlo: %v", err)
	}
}
