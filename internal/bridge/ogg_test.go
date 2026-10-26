package bridge

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestOggSpeechJoinsSegmentsAndSilence(t *testing.T) {
	var o oggSpeech
	var joined []byte
	for _, serial := range []uint32{17, 29} {
		o.next()
		for _, b := range speechFixture(serial) {
			out, err := o.feed([]byte{b})
			if err != nil {
				t.Fatal(err)
			}
			joined = append(joined, out...)
		}
		if err := o.endSource(); err != nil {
			t.Fatal(err)
		}
		if serial == 17 {
			joined = append(joined, o.silence()...)
		}
	}
	joined = append(joined, o.finish()...)
	var sequence uint32
	var previous uint64
	var heads, eos int
	for len(joined) > 0 {
		n := int(joined[26])
		size := 27 + n
		for _, s := range joined[27 : 27+n] {
			size += int(s)
		}
		p := joined[:size]
		joined = joined[size:]
		if binary.LittleEndian.Uint32(p[14:18]) != 17 || binary.LittleEndian.Uint32(p[18:22]) != sequence {
			t.Fatal("stream identity or sequence changed")
		}
		sequence++
		if oggChecksum(p) != binary.LittleEndian.Uint32(p[22:26]) {
			t.Fatal("bad checksum")
		}
		g := binary.LittleEndian.Uint64(p[6:14])
		if g < previous {
			t.Fatal("time moved backwards")
		}
		previous = g
		if bytes.HasPrefix(p[27+n:], []byte("OpusHead")) {
			heads++
		}
		if p[5]&4 != 0 {
			eos++
			if len(joined) > 0 {
				t.Fatal("EOS before last audio")
			}
		}
	}
	if heads != 1 || eos != 1 || previous != 6720 {
		t.Fatalf("heads=%d EOS=%d duration=%d", heads, eos, previous)
	}
}
func TestOggSpeechRejectsBrokenInput(t *testing.T) {
	for _, b := range [][]byte{bytes.Repeat([]byte{1}, 27), append([]byte{}, speechFixture(1)...)} {
		if string(b[:4]) == "OggS" {
			b[len(b)-1] ^= 1
		}
		var o oggSpeech
		if _, err := o.feed(b); err == nil {
			t.Fatal("invalid page accepted")
		}
	}
	var o oggSpeech
	if _, err := o.feed(speechFixture(1)[:30]); err != nil {
		t.Fatal(err)
	}
	if err := o.endSource(); err == nil {
		t.Fatal("truncated segment accepted")
	}
}
func TestOpusPacketDurations(t *testing.T) {
	for _, tc := range []struct {
		p       []byte
		samples uint64
	}{{[]byte{0xf8, 0xff, 0xfe}, 960}, {[]byte{0x80}, 120}, {[]byte{0x18}, 2880}, {[]byte{0x79}, 1920}, {[]byte{0x83, 2}, 240}} {
		got, err := opusSamples(tc.p)
		if err != nil || got != tc.samples {
			t.Fatalf("%x samples=%d err=%v", tc.p, got, err)
		}
	}
	for _, p := range [][]byte{nil, {3}, {3, 0}, {0x1b, 3}} {
		if _, err := opusSamples(p); err == nil {
			t.Fatalf("invalid packet accepted: %x", p)
		}
	}
}

func TestOggSpeechTrimsFinalPacketWithoutRegressingGranule(t *testing.T) {
	src := oggSpeech{serial: 1}
	head := append([]byte("OpusHead"), []byte{1, 1, 56, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}...)
	b := src.page(head, 2, 0)
	b = append(b, src.page(append([]byte("OpusTags"), make([]byte, 8)...), 0, 0)...)
	b = append(b, src.page([]byte{0xf8, 0xff, 0xfe}, 0, 960)...)
	b = append(b, src.page([]byte{0xf8, 0xff, 0xfe}, 4, 1600)...)
	var o oggSpeech
	joined, err := o.feed(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.endSource(); err != nil {
		t.Fatal(err)
	}
	joined = append(joined, o.finish()...)
	previous := uint64(0)
	for len(joined) > 0 {
		count := int(joined[26])
		size := 27 + count
		for _, n := range joined[27 : 27+count] {
			size += int(n)
		}
		page := joined[:size]
		joined = joined[size:]
		granule := binary.LittleEndian.Uint64(page[6:14])
		if granule < previous {
			t.Fatal("EOS moved time backwards")
		}
		previous = granule
		if page[5]&4 != 0 && (count == 0 || len(joined) > 0) {
			t.Fatal("EOS must carry final audio packet")
		}
	}
	if previous != 1600 {
		t.Fatalf("end padding not trimmed: %d", previous)
	}
}
