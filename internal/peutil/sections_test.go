package peutil

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExecutableRangesReturnsOnlyExecutableSections(t *testing.T) {
	t.Parallel()

	path := writePESample(t)

	ranges, err := ExecutableRanges(path)
	if err != nil {
		t.Fatalf("ExecutableRanges returned error: %v", err)
	}

	expected := []Range{
		{Start: 0x200, End: 0x400},
	}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("ExecutableRanges returned %v, want %v", ranges, expected)
	}
}

func TestExecutableRangesReturnsEmptySliceWhenNoExecutableSection(t *testing.T) {
	t.Parallel()

	path := writePESample(t, withoutExecutableSection())

	ranges, err := ExecutableRanges(path)
	if err != nil {
		t.Fatalf("ExecutableRanges returned error: %v", err)
	}
	if ranges == nil {
		t.Fatalf("ExecutableRanges returned nil slice, want empty slice")
	}
	if len(ranges) != 0 {
		t.Fatalf("ExecutableRanges returned %v, want empty slice", ranges)
	}
}

type peSampleOption func(*peSampleConfig)

type peSampleConfig struct {
	execCharacteristics uint32
}

func withoutExecutableSection() peSampleOption {
	return func(cfg *peSampleConfig) {
		cfg.execCharacteristics = 0x40000040
	}
}

func writePESample(t *testing.T, opts ...peSampleOption) string {
	t.Helper()

	cfg := peSampleConfig{
		execCharacteristics: 0x60000020,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	const (
		fileAlignment       = 0x200
		sectionAlignment    = 0x1000
		sizeOfHeaders       = 0x200
		textRawPointer      = 0x200
		textRawSize         = 0x200
		rdataRawPointer     = 0x400
		rdataRawSize        = 0x200
		optionalHeaderSize  = 0xE0
		peHeaderOffset      = 0x80
		numberOfSections    = 2
		sizeOfImage         = 0x3000
		addressOfEntryPoint = 0x1000
	)

	buffer := make([]byte, 0x600)
	copy(buffer[0:2], []byte{'M', 'Z'})
	binary.LittleEndian.PutUint32(buffer[0x3C:0x40], peHeaderOffset)
	copy(buffer[peHeaderOffset:peHeaderOffset+4], []byte{'P', 'E', 0, 0})

	fileHeader := make([]byte, 20)
	binary.LittleEndian.PutUint16(fileHeader[0:2], 0x14c)
	binary.LittleEndian.PutUint16(fileHeader[2:4], numberOfSections)
	binary.LittleEndian.PutUint16(fileHeader[16:18], optionalHeaderSize)
	binary.LittleEndian.PutUint16(fileHeader[18:20], 0x210E)
	copy(buffer[peHeaderOffset+4:peHeaderOffset+24], fileHeader)

	optionalHeader := make([]byte, optionalHeaderSize)
	binary.LittleEndian.PutUint16(optionalHeader[0:2], 0x10b)
	optionalHeader[2] = 1
	binary.LittleEndian.PutUint32(optionalHeader[16:20], addressOfEntryPoint)
	binary.LittleEndian.PutUint32(optionalHeader[20:24], 0x1000)
	binary.LittleEndian.PutUint32(optionalHeader[24:28], 0x2000)
	binary.LittleEndian.PutUint32(optionalHeader[28:32], 0x400000)
	binary.LittleEndian.PutUint32(optionalHeader[32:36], sectionAlignment)
	binary.LittleEndian.PutUint32(optionalHeader[36:40], fileAlignment)
	binary.LittleEndian.PutUint32(optionalHeader[56:60], sizeOfImage)
	binary.LittleEndian.PutUint32(optionalHeader[60:64], sizeOfHeaders)
	binary.LittleEndian.PutUint16(optionalHeader[68:70], 3)
	binary.LittleEndian.PutUint32(optionalHeader[92:96], 16)
	sectionTableOffset := peHeaderOffset + 24 + optionalHeaderSize
	copy(buffer[peHeaderOffset+24:sectionTableOffset], optionalHeader)

	textSection := make([]byte, 40)
	copy(textSection[0:8], []byte(".text"))
	binary.LittleEndian.PutUint32(textSection[8:12], 0x180)
	binary.LittleEndian.PutUint32(textSection[12:16], 0x1000)
	binary.LittleEndian.PutUint32(textSection[16:20], textRawSize)
	binary.LittleEndian.PutUint32(textSection[20:24], textRawPointer)
	binary.LittleEndian.PutUint32(textSection[36:40], cfg.execCharacteristics)
	copy(buffer[sectionTableOffset:sectionTableOffset+40], textSection)

	rdataSection := make([]byte, 40)
	copy(rdataSection[0:8], []byte(".rdata"))
	binary.LittleEndian.PutUint32(rdataSection[8:12], 0x100)
	binary.LittleEndian.PutUint32(rdataSection[12:16], 0x2000)
	binary.LittleEndian.PutUint32(rdataSection[16:20], rdataRawSize)
	binary.LittleEndian.PutUint32(rdataSection[20:24], rdataRawPointer)
	binary.LittleEndian.PutUint32(rdataSection[36:40], 0x40000040)
	copy(buffer[sectionTableOffset+40:sectionTableOffset+80], rdataSection)

	copy(buffer[textRawPointer:textRawPointer+textRawSize], bytes.Repeat([]byte{0x90}, textRawSize))
	copy(buffer[rdataRawPointer:rdataRawPointer+rdataRawSize], bytes.Repeat([]byte{0xCC}, rdataRawSize))

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "sample.dll")
	if err := os.WriteFile(path, buffer, 0o600); err != nil {
		t.Fatalf("write sample PE: %v", err)
	}

	return path
}
