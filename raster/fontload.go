package raster

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// FontSpec names the faces to draw text with. Characters a face cannot
// draw are drawn, one by one, with Hiragino Sans (W3 for body text, W6 for
// bold); a character no face can draw is an error.
type FontSpec struct {
	// Path is a font file: .ttf, .otf or .ttc.
	Path string
	// Name picks a face in the file by its full name (name ID 4) or its
	// PostScript name (ID 6), in any language the file records, ignoring
	// case: "HiraginoSans-W6" and "ヒラギノ角ゴシック W6" both work. Empty
	// means the file's first face.
	Name string
	// BoldPath and BoldName are the bold face (entity names, frame titles).
	// An empty BoldPath means the body face.
	BoldPath string
	BoldName string
}

// LoadFont loads the faces spec names, with Hiragino as the fallback when
// the system has it. Load once and share the result.
func LoadFont(spec FontSpec) (*Font, error) {
	if spec.Path == "" {
		return nil, errors.New("font: no path")
	}
	body, err := openFace(spec.Path, spec.Name)
	if err != nil {
		return nil, err
	}
	bold := body
	if spec.BoldPath != "" {
		if bold, err = openFace(spec.BoldPath, spec.BoldName); err != nil {
			return nil, err
		}
	}
	fn := &Font{body: []*face{body}, bold: []*face{bold}}
	// Hiragino fills in, character by character. Without it (not macOS),
	// the chosen faces stand alone and a character they lack is an error.
	if w3, err := loadFace(hiraginoW3, 0); err == nil {
		fn.body = append(fn.body, w3)
	}
	if w6, err := loadFace(hiraginoW6, 0); err == nil {
		fn.bold = append(fn.bold, w6)
	}
	return fn, nil
}

// openFace opens the face of path that name picks (the first face for "").
// Errors name the face, not only the file: in a collection one face may be
// readable and the next not.
func openFace(path, name string) (*face, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	offsets, err := faceOffsets(b)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	index := 0
	if name != "" {
		index = -1
		var seen []string
		for i, off := range offsets {
			names, err := faceNames(b, off)
			if err != nil {
				seen = append(seen, fmt.Sprintf("face %d: %v", i, err))
				continue
			}
			for _, n := range names {
				if strings.EqualFold(n.text, name) && (n.id == 4 || n.id == 6) {
					index = i
				}
			}
			if index == i {
				break
			}
			seen = append(seen, faceLabel(names, i))
		}
		if index < 0 {
			return nil, fmt.Errorf("font %s: no face named %q; the faces are: %s", path, name, strings.Join(seen, ", "))
		}
	}
	c, err := opentype.ParseCollection(b)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	sf, err := c.Font(index)
	if err != nil {
		label := fmt.Sprintf("face %d", index)
		if names, nerr := faceNames(b, offsets[index]); nerr == nil {
			label = faceLabel(names, index)
		}
		return nil, fmt.Errorf("font %s, %s: %w", path, label, err)
	}
	return &face{sf: sf, name: path, sized: map[float64]font.Face{}, ok: map[rune]bool{}}, nil
}

// faceLabel is how a face is named in an error: its PostScript name, else
// its full name, else its index.
func faceLabel(names []faceName, i int) string {
	for _, id := range []uint16{6, 4} {
		for _, n := range names {
			if n.id == id && n.text != "" {
				return fmt.Sprintf("%q", n.text)
			}
		}
	}
	return fmt.Sprintf("face %d", i)
}

// faceOffsets are the offsets of a file's faces: one for a font file, one
// per face for a collection ("ttcf").
func faceOffsets(b []byte) ([]uint32, error) {
	if len(b) < 12 {
		return nil, errors.New("too short to be a font")
	}
	if string(b[:4]) != "ttcf" {
		return []uint32{0}, nil
	}
	n := binary.BigEndian.Uint32(b[8:])
	if n == 0 || uint64(12)+uint64(n)*4 > uint64(len(b)) {
		return nil, fmt.Errorf("collection header lists %d faces the file does not hold", n)
	}
	offs := make([]uint32, n)
	for i := range offs {
		offs[i] = binary.BigEndian.Uint32(b[12+4*i:])
	}
	return offs, nil
}

type faceName struct {
	id   uint16
	text string
}

// faceNames reads every record of a face's name table, in every language
// (sfnt.Font.Name returns only the first record for an ID). Windows and
// Unicode records are UTF-16BE; Macintosh Roman records are decoded too;
// other Macintosh encodings are skipped.
func faceNames(b []byte, off uint32) ([]faceName, error) {
	u16 := func(at uint64) (uint16, bool) {
		if at+2 > uint64(len(b)) {
			return 0, false
		}
		return binary.BigEndian.Uint16(b[at:]), true
	}
	u32 := func(at uint64) (uint32, bool) {
		if at+4 > uint64(len(b)) {
			return 0, false
		}
		return binary.BigEndian.Uint32(b[at:]), true
	}
	o := uint64(off)
	numTables, ok := u16(o + 4)
	if !ok {
		return nil, errors.New("table directory out of range")
	}
	var nameOff, nameLen uint32
	found := false
	for i := uint64(0); i < uint64(numTables); i++ {
		rec := o + 12 + 16*i
		if rec+16 > uint64(len(b)) {
			return nil, errors.New("table directory out of range")
		}
		if string(b[rec:rec+4]) == "name" {
			nameOff, _ = u32(rec + 8)
			nameLen, _ = u32(rec + 12)
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("no name table")
	}
	t := uint64(nameOff)
	if t+uint64(nameLen) > uint64(len(b)) || nameLen < 6 {
		return nil, errors.New("name table out of range")
	}
	count, _ := u16(t + 2)
	strOff, _ := u16(t + 4)
	var out []faceName
	decoded := 0
	for i := uint64(0); i < uint64(count); i++ {
		r := t + 6 + 12*i
		if r+12 > t+uint64(nameLen) {
			return nil, errors.New("name records out of range")
		}
		platform, _ := u16(r)
		encoding, _ := u16(r + 2)
		id, _ := u16(r + 6)
		if id != 4 && id != 6 {
			continue // only the names a face is chosen and reported by
		}
		length, _ := u16(r + 8)
		sOff, _ := u16(r + 10)
		s := t + uint64(strOff) + uint64(sOff)
		if s+uint64(length) > t+uint64(nameLen) {
			return nil, errors.New("name string out of range")
		}
		raw := b[s : s+uint64(length)]
		if decoded += len(raw); decoded > maxNameBytes {
			// Records may all point at one long string: decoding each
			// would cost gigabytes for a small file.
			return nil, errors.New("name table too large")
		}
		var text string
		switch {
		case platform == 0 || platform == 3:
			if len(raw)%2 != 0 {
				continue
			}
			u := make([]uint16, len(raw)/2)
			for k := range u {
				u[k] = binary.BigEndian.Uint16(raw[2*k:])
			}
			text = string(utf16.Decode(u))
		case platform == 1 && encoding == 0:
			text = macRoman(raw)
		default:
			continue
		}
		out = append(out, faceName{id: id, text: text})
	}
	return out, nil
}

// maxNameBytes bounds the name strings one face's table may make us decode.
const maxNameBytes = 64 << 10

// macRoman decodes Macintosh Roman.
func macRoman(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
			continue
		}
		sb.WriteRune(macRomanHigh[c-0x80])
	}
	return sb.String()
}

var macRomanHigh = []rune("ÄÅÇÉÑÖÜáàâäãåçéèêëíìîïñóòôöõúùûü†°¢£§•¶ß®©™´¨≠ÆØ∞±≤≥¥µ∂∑∏π∫ªºΩæø" +
	"¿¡¬√ƒ≈∆«»…\u00A0ÀÃÕŒœ–—“”‘’÷◊ÿŸ⁄€‹›\uFB01\uFB02‡·‚„‰ÂÊÁËÈÍÎÏÌÓÔ\uF8FFÒÚÛÙıˆ˜¯˘˙˚¸˝˛ˇ")
