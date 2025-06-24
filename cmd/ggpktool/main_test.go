package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"encoding/binary" // Added import

	// Need to import ggpk to potentially create a test GGPK file
	// If we use a pre-existing binary GGPK for testing, this might not be needed directly here
	// but rather a helper that provides the path to such a file.
	// For now, let's assume we might build one.
	"github.com/user/ggpkgo/pkg/ggpk" // To build a test GGPK
	// "io/ioutil" // For ReadFile, but os.ReadFile is preferred now
)

// Helper to build a minimal GGPK file for testing
// This is a simplified version. A more robust one would be in a test shared package.
func createTestGGPKFile(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	ggpkPath := filepath.Join(tmpDir, "test.ggpk")

	// Minimal GGPK structure: GGPK_Header -> Root_PDIR -> File1_FILE
	// Offsets need to be calculated carefully.

	var fileContent bytes.Buffer

	// GGPK Record (28 bytes)
	// Length: 28, Tag: GGPK, Version: 3, RootOffset: 28, FreeOffset: 0
	ggpkHeader := struct {
		Length uint32
		Tag    uint32
		Version uint32
		RootOff int64
		FreeOff int64
	}{28, ggpk.GGPKRecordTag, 3, 28, 0}
	binary.Write(&fileContent, ggpk.GGPKEndian, &ggpkHeader)

	// Root Directory Record (PDIR)
	// Assume it contains one file "file1.txt"
	// PDIR Header: Length, Tag (PDIR), NameLength, EntryCount, Hash
	// Name: "" (null terminated)
	// Entry: NameHash (file1.txt), Offset (to FILE record)

	// File1.txt content
	file1Data := []byte("hello world from GGPK")

	// File1 Record (FILE)
	// FILE Header: Length, Tag (FILE), NameLength, Hash
	// Name: "file1.txt" (null terminated)
	// Data: file1Data

	// Calculate offsets backwards or define them
	file1Name := "file1.txt"
	file1NameLenChars := uint32(len(file1Name) + 1) // including null
	file1NameBytesLen := file1NameLenChars * 2 // UTF-16

	file1RecordHeaderSize := uint32(4+4+4+ggpk.HashSize) + file1NameBytesLen
	file1RecordLength := file1RecordHeaderSize + uint32(len(file1Data))

	// Root PDIR contains 1 entry (NameHash + Offset = 4 + 8 = 12 bytes)
	rootDirName := "" // Root dir is unnamed in record
	rootDirNameLenChars := uint32(len(rootDirName) + 1) // for null
	rootDirNameBytesLen := rootDirNameLenChars * 2 // UTF-16 for version 3

	rootDirHeaderSize := uint32(4+4+4+4+ggpk.HashSize) + rootDirNameBytesLen
	rootDirLength := rootDirHeaderSize + 12 // 12 for one entry

	// Offsets:
	// GGPK Header: 0 (size 28)
	// Root PDIR: 28 (size rootDirLength)
	// File1 FILE: 28 + rootDirLength (size file1RecordLength)

	offsetFile1 := int64(28 + rootDirLength)

	// Write Root PDIR
	binary.Write(&fileContent, ggpk.GGPKEndian, rootDirLength)
	binary.Write(&fileContent, ggpk.GGPKEndian, uint32(ggpk.PDirRecordTag))
	binary.Write(&fileContent, ggpk.GGPKEndian, rootDirNameLenChars)
	binary.Write(&fileContent, ggpk.GGPKEndian, uint32(1)) // EntryCount
	var rootDirHash [ggpk.HashSize]byte; rootDirHash[0] = 0xAA; // Dummy hash
	fileContent.Write(rootDirHash[:])
	// Write root dir name (empty string + null terminator for UTF-16)
	for i := uint32(0); i < rootDirNameLenChars-1; i++ { fileContent.WriteByte(0); fileContent.WriteByte(0); }
	fileContent.WriteByte(0); fileContent.WriteByte(0); // Null terminator

	// Write Root PDIR Entry for file1.txt
	// Dummy NameHash for "file1.txt" - actual hashing not critical for this structure test
	file1NameHash := uint32(0x12345678)
	binary.Write(&fileContent, ggpk.GGPKEndian, file1NameHash)
	binary.Write(&fileContent, ggpk.GGPKEndian, offsetFile1)


	// Write File1 Record
	binary.Write(&fileContent, ggpk.GGPKEndian, file1RecordLength)
	binary.Write(&fileContent, ggpk.GGPKEndian, uint32(ggpk.FileRecordTag))
	binary.Write(&fileContent, ggpk.GGPKEndian, file1NameLenChars)
	var file1Hash [ggpk.HashSize]byte; file1Hash[0] = 0xBB; // Dummy hash
	fileContent.Write(file1Hash[:])
	// Write file1.txt name (UTF-16)
	for _, r := range file1Name {
		binary.Write(&fileContent, ggpk.GGPKEndian, uint16(r))
	}
	binary.Write(&fileContent, ggpk.GGPKEndian, uint16(0)) // Null terminator
	// Write file1.txt data
	fileContent.Write(file1Data)

	// Write the complete GGPK to a temporary file
	err := os.WriteFile(ggpkPath, fileContent.Bytes(), 0644)
	if err != nil {
		t.Fatalf("Failed to write test GGPK file: %v", err)
	}
	return ggpkPath
}


func TestGGPKTool_ListAction(t *testing.T) {
	ggpkFilePath := createTestGGPKFile(t)

	// Build the ggpktool binary
	cmdName := "ggpktool_test_list" // Unique name for parallel tests if any
	if os.PathSeparator == '\\' { // Windows
		cmdName += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", cmdName, ".") // Build in current dir (cmd/ggpktool)
	buildCmd.Dir = "." // Ensure it builds from the ggpktool cmd directory
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build ggpktool: %v\nOutput: %s", err, string(output))
	}
	defer os.Remove(cmdName) // Clean up the built binary

	// Run the list action
	runCmd := exec.Command("./"+cmdName, "-ggpk", ggpkFilePath, "-action", "list")
	listOutputBytes, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ggpktool list action failed: %v\nOutput: %s", err, string(listOutputBytes))
	}

	listOutput := string(listOutputBytes)
	// fmt.Println("List output:\n", listOutput) // For debugging

	// Basic checks for output
	if !strings.Contains(listOutput, "GGPK Tool - Go Version") {
		t.Error("Output missing tool header")
	}
	if !strings.Contains(listOutput, "Processing GGPK file:") {
		t.Error("Output missing processing message")
	}
	if !strings.Contains(listOutput, "/") { // Root directory
		t.Error("Output missing root directory listing '/'")
	}
	if !strings.Contains(listOutput, "file1.txt") {
		t.Error("Output missing 'file1.txt' listing")
	}
}


func TestGGPKTool_ExtractAction(t *testing.T) {
	ggpkFilePath := createTestGGPKFile(t)
	outputDir := t.TempDir() // Create a unique temp dir for output

	cmdName := "ggpktool_test_extract"
	if os.PathSeparator == '\\' {
		cmdName += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", cmdName, ".")
	buildCmd.Dir = "."
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build ggpktool: %v\nOutput: %s", err, string(output))
	}
	defer os.Remove(cmdName)

	targetPathInGGPK := "file1.txt"
	// The output will be outputDir/file1.txt because -out is a directory.
	// The CLI tool's extractFile joins outputPath with Base(itemPath)
	expectedOutputFile := filepath.Join(outputDir, targetPathInGGPK)


	runCmd := exec.Command("./"+cmdName, "-ggpk", ggpkFilePath, "-action", "extract", "-path", targetPathInGGPK, "-out", outputDir)
	extractOutputBytes, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ggpktool extract action failed: %v\nOutput: %s", err, string(extractOutputBytes))
	}
	// fmt.Println("Extract output:\n", string(extractOutputBytes))


	if _, err := os.Stat(expectedOutputFile); os.IsNotExist(err) {
		t.Fatalf("Expected output file '%s' does not exist", expectedOutputFile)
	}

	content, err := os.ReadFile(expectedOutputFile)
	if err != nil {
		t.Fatalf("Failed to read extracted file '%s': %v", expectedOutputFile, err)
	}
	expectedContent := "hello world from GGPK"
	if string(content) != expectedContent {
		t.Errorf("Extracted file content mismatch. Expected '%s', got '%s'", expectedContent, string(content))
	}
}

func TestGGPKTool_ExtractAllAction(t *testing.T) {
	ggpkFilePath := createTestGGPKFile(t)
	outputDir := t.TempDir()

	cmdName := "ggpktool_test_extract_all"
	if os.PathSeparator == '\\' {
		cmdName += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", cmdName, ".")
	buildCmd.Dir = "."
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build ggpktool: %v\nOutput: %s", err, string(output))
	}
	defer os.Remove(cmdName)

	runCmd := exec.Command("./"+cmdName, "-ggpk", ggpkFilePath, "-action", "extract-all", "-out", outputDir)
	extractOutputBytes, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ggpktool extract-all action failed: %v\nOutput: %s", err, string(extractOutputBytes))
	}
	// fmt.Println("Extract-all output:\n", string(extractOutputBytes))

	// In our test GGPK, file1.txt is at the root.
	// extractAllFiles joins baseOutputDir with node.GetPath().
	// Root node's GetPath() is "", its children's GetPath() will be just their name.
	expectedOutputFile := filepath.Join(outputDir, "file1.txt")
	if _, err := os.Stat(expectedOutputFile); os.IsNotExist(err) {
		t.Fatalf("Expected output file '%s' from extract-all does not exist", expectedOutputFile)
	}

	content, err := os.ReadFile(expectedOutputFile)
	if err != nil {
		t.Fatalf("Failed to read extracted file '%s' from extract-all: %v", expectedOutputFile, err)
	}
	expectedContent := "hello world from GGPK"
	if string(content) != expectedContent {
		t.Errorf("Extracted file content mismatch from extract-all. Expected '%s', got '%s'", expectedContent, string(content))
	}
}


// TODO: Add tests for cmd/extractbundledggpk (more complex due to needing bundle files)
// TODO: Add basic invocation tests for cmd/browseggpk
