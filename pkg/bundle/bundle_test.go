package bundle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/new-world-tools/go-oodle" // For testing Oodle decompression calls
	// "github.com/aviddiviner/go-murmur" // Not directly used in tests unless testing NameHash explicitly here
)

// Helper to create a temporary file with given bytes
func createTempBundleFile(t *testing.T, content []byte) (string, func()) {
	t.Helper()
	// Use t.TempDir() to ensure cleanup even if test panics or calls t.Fatal
	tmpDir := t.TempDir()
	tmpFile, err := os.Create(filepath.Join(tmpDir, "test.bundle.bin"))
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	fileName := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	return fileName, func() { /* os.Remove(fileName) is handled by t.TempDir() */ }
}

// TestOpenBundleFile_HeaderParsing tests parsing of the BundleHeader.
func TestOpenBundleFile_HeaderParsing(t *testing.T) {
	header := BundleHeader{
		UncompressedSize:    1024,
		CompressedSize:      512,
		HeadSize:            48 + 4*2, // 48 + chunk_count * 4 (assuming 2 chunks)
		Compressor:          int32(OodleCompressorLeviathan),
		Unknown1:            1,
		UncompressedSizeLong: 1024,
		CompressedSizeLong:  512,
		ChunkCount:          2,
		ChunkSize:           262144,
	}
	chunkSizes := []int32{256, 256}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, &header)
	binary.Write(&buf, binary.LittleEndian, &chunkSizes)
	// No actual chunk data needed for this header test

	filePath, _ := createTempBundleFile(t, buf.Bytes())
	// defer cleanup() // t.TempDir() handles cleanup

	bundle, err := OpenBundleFile(filePath, nil, false)
	if err != nil {
		t.Fatalf("OpenBundleFile failed: %v", err)
	}
	defer bundle.Close()

	if bundle.Header.UncompressedSize != header.UncompressedSize {
		t.Errorf("Expected UncompressedSize %d, got %d", header.UncompressedSize, bundle.Header.UncompressedSize)
	}
	if bundle.Header.Compressor != header.Compressor {
		t.Errorf("Expected Compressor %d, got %d", header.Compressor, bundle.Header.Compressor)
	}
	if bundle.Header.ChunkCount != header.ChunkCount {
		t.Errorf("Expected ChunkCount %d, got %d", header.ChunkCount, bundle.Header.ChunkCount)
	}
	if len(bundle.CompressedChunkSizes) != int(header.ChunkCount) {
		t.Errorf("Expected %d chunk sizes, got %d", header.ChunkCount, len(bundle.CompressedChunkSizes))
	}
	if len(bundle.CompressedChunkSizes) > 0 && bundle.CompressedChunkSizes[0] != chunkSizes[0] {
		t.Errorf("Expected first chunk size %d, got %d", chunkSizes[0], bundle.CompressedChunkSizes[0])
	}
}

// TestBundle_ReadFull_OodleNone tests reading an uncompressed bundle (OodleCompressorNone).
func TestBundle_ReadFull_OodleNone(t *testing.T) {
	chunk1Data := []byte(strings.Repeat("A", 100))
	chunk2Data := []byte(strings.Repeat("B", 50))
	uncompressedSize := int32(len(chunk1Data) + len(chunk2Data))

	header := BundleHeader{
		UncompressedSize:    uncompressedSize,
		CompressedSize:      uncompressedSize, // Same for OodleNone
		HeadSize:            48 + 4*2,
		Compressor:          int32(OodleCompressorNone),
		Unknown1:            1,
		UncompressedSizeLong: int64(uncompressedSize),
		CompressedSizeLong:  int64(uncompressedSize),
		ChunkCount:          2,
		ChunkSize:           100, // ChunkSize matching first chunk for simplicity
	}
	// For OodleNone, compressed chunk size == uncompressed chunk size for that chunk
	chunkSizes := []int32{int32(len(chunk1Data)), int32(len(chunk2Data))}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, &header)
	binary.Write(&buf, binary.LittleEndian, &chunkSizes)
	buf.Write(chunk1Data)
	buf.Write(chunk2Data)

	filePath, _ := createTempBundleFile(t, buf.Bytes())
	bundle, err := OpenBundleFile(filePath, nil, false)
	if err != nil {
		t.Fatalf("OpenBundleFile failed: %v", err)
	}
	defer bundle.Close()

	fullData, err := bundle.ReadFull()
	if err != nil {
		t.Fatalf("Bundle.ReadFull failed: %v", err)
	}

	expectedData := append(chunk1Data, chunk2Data...)
	if !bytes.Equal(fullData, expectedData) {
		t.Errorf("ReadFull data mismatch. Expected %d bytes, got %d bytes.", len(expectedData), len(fullData))
		// t.Logf("Expected: %x\nGot:      %x", expectedData, fullData)
	}

	// Test ReadAt
	readAtData, err := bundle.ReadAt(int32(len(chunk1Data))-10, 20) // Read across chunk boundary
	if err != nil {
		t.Fatalf("Bundle.ReadAt failed: %v", err) // Changed %w to %v
	}
	expectedReadAt := expectedData[len(chunk1Data)-10 : len(chunk1Data)-10+20]
	if !bytes.Equal(readAtData, expectedReadAt) {
		t.Errorf("ReadAt data mismatch")
	}
}


// TestIndex_NameHash tests the NameHash function with FNV1a.
// MurmurHash testing would require known test vectors for that specific variant.
func TestIndex_NameHash_FNV1a(t *testing.T) {
	idx := &Index{
		// Mock a directory record that indicates FNV1a usage
		Directories: []IndexDirectoryRecord{{PathHash: 0x07E47507B4A92E53}},
	}
	// Example paths and their expected FNV1a64 (custom PoE version) hashes
	// These hashes would need to be pre-calculated using an identical FNV algorithm
	// to the one in C# or the one implemented in Go.
	// For now, this tests if the function runs and produces A hash.
	// Real validation requires known good hash values.
	testPaths := []struct{ path string; note string }{
		{"Path/To/File.txt", "simple path"},
		{"ROOT/SomethingElse/", "trailing slash"}, // Trailing slash should be trimmed by NameHash
		{"Data/UPPERCASE.DAT", "uppercase"},
	}

	for _, tc := range testPaths {
		t.Run(tc.path, func(t *testing.T) {
			hash, err := idx.NameHash(tc.path)
			if err != nil {
				t.Fatalf("NameHash failed for '%s': %v", tc.path, err)
			}
			if hash == 0 { // Basic check, real check needs known values
				t.Errorf("NameHash for '%s' produced 0, unexpected for FNV1a", tc.path)
			}
			// t.Logf("Path: '%s', FNV1a Hash: %X (%s)", tc.path, hash, tc.note)

			// Test path with trailing slash if original didn't have one
			if !strings.HasSuffix(tc.path, "/") {
				hashWithSlash, err := idx.NameHash(tc.path + "/")
				if err != nil {
					t.Fatalf("NameHash failed for '%s/': %v", tc.path, err)
				}
				if hashWithSlash != hash {
					t.Errorf("NameHash for '%s' (%X) and '%s/' (%X) should be identical due to trimming", tc.path, hash, tc.path+"/", hashWithSlash)
				}
			}
		})
	}
}

// TestIndex_NameHash_Murmur (Placeholder)
// This test will fail or be inaccurate until a proper MurmurHash64A is implemented
// and known test vectors are available.
func TestIndex_NameHash_Murmur(t *testing.T) {
	t.Skip("Skipping MurmurHash test: placeholder implementation or requires known vectors.")
	idx := &Index{
		Directories: []IndexDirectoryRecord{{PathHash: 0xF42A94E69CFF42FE}},
	}
	path := "Art/Models/Model.geo"
	// lowerPath := "art/models/model.geo" // Murmur hashes lowercase version
	hash, err := idx.NameHash(path)
	if err != nil {
		t.Fatalf("NameHash (Murmur) failed for '%s': %v", path, err)
	}
	// Add known hash value for "art/models/model.geo" with seed 0x1337B33F if available
	// For now, just check it runs.
	if hash == 0 && path != "" {
		t.Errorf("NameHash (Murmur) for '%s' produced 0", path)
	}
	// t.Logf("Path: '%s', MurmurHash64A Hash (placeholder): %X", path, hash)
}


// --- Mocking for OpenIndex and ParsePaths ---
// Create a minimal, uncompressed index bundle content for testing OpenIndex and ParsePaths
func createMockIndexBundleContent(t *testing.T, numBundles, numFilesPerBundle, numDirs int) []byte {
	var indexContent bytes.Buffer

	// BundleRecords
	binary.Write(&indexContent, binary.LittleEndian, int32(numBundles))
	bundleRecords := make([]*IndexBundleRecord, numBundles)
	for i := 0; i < numBundles; i++ {
		path := fmt.Sprintf("Bundle%d", i)
		pathLen := int32(len(path))
		uncompressedSize := int32(1000 * (i + 1))
		binary.Write(&indexContent, binary.LittleEndian, pathLen)
		indexContent.Write([]byte(path))
		binary.Write(&indexContent, binary.LittleEndian, uncompressedSize)
		bundleRecords[i] = &IndexBundleRecord{Path: path, UncompressedSize: uncompressedSize, BundleIndex: i}
	}

	// FileRecords
	totalFiles := numBundles * numFilesPerBundle
	binary.Write(&indexContent, binary.LittleEndian, int32(totalFiles))
	fileRecords := make([]*IndexFileRecord, totalFiles)
	fileCounter := 0
	for i := 0; i < numBundles; i++ {
		for j := 0; j < numFilesPerBundle; j++ {
			// Path will be set by ParsePaths. PathHash needs to be consistent.
			// For testing ParsePaths, we need actual paths and their hashes.
			// For testing OpenIndex structure, dummy values are okay.
			pathHash := uint64(0x1000000000000000 + fileCounter) // Dummy unique hash
			bundleIndex := int32(i)
			offset := int32(j * 100)
			size := int32(50)
			binary.Write(&indexContent, binary.LittleEndian, pathHash)
			binary.Write(&indexContent, binary.LittleEndian, bundleIndex)
			binary.Write(&indexContent, binary.LittleEndian, offset)
			binary.Write(&indexContent, binary.LittleEndian, size)
			fileRecords[fileCounter] = &IndexFileRecord{PathHash: pathHash, BundleRecord: bundleRecords[i], Offset: offset, Size: size}
			fileCounter++
		}
	}

	// DirectoryRecords (for Index.Directories)
	binary.Write(&indexContent, binary.LittleEndian, int32(numDirs))
	for i := 0; i < numDirs; i++ {
		dirRec := IndexDirectoryRecord{
			PathHash:      uint64(0x2000000000000000 + i), // Dummy
			Offset:        int32(i * 10), // Dummy offset into DirectoryBundleData
			Size:          int32(10),      // Dummy size of this dir's data in DirectoryBundleData
			RecursiveSize: int32(20),     // Dummy
		}
		binary.Write(&indexContent, binary.LittleEndian, &dirRec)
	}

	// DirectoryBundleData (placeholder, actual content needed for ParsePaths test)
	// For now, just a few empty bytes. A real ParsePaths test needs this to be meaningful.
	indexContent.Write(make([]byte, numDirs*10)) // Dummy data matching offsets/sizes above

	return indexContent.Bytes()
}

func TestOpenIndex_Structure(t *testing.T) {
	numBundles := 2
	numFilesPerBundle := 3
	numDirs := 1
	mockIndexData := createMockIndexBundleContent(t, numBundles, numFilesPerBundle, numDirs)

	// Wrap mockIndexData in a Bundle structure (uncompressed for this test)
	header := BundleHeader{
		UncompressedSize: int32(len(mockIndexData)),
		CompressedSize:   int32(len(mockIndexData)),
		HeadSize:         48, // 0 chunks for this simple wrapper
		Compressor:       int32(OodleCompressorNone),
		Unknown1:         1,
		UncompressedSizeLong: int64(len(mockIndexData)),
		CompressedSizeLong:  int64(len(mockIndexData)),
		ChunkCount:       0, // If ChunkCount is 0, ReadFull should handle it. Or 1 if data exists.
		ChunkSize:        262144,
	}
	if len(mockIndexData) > 0 {
		header.ChunkCount = 1 // One chunk containing all data
	}


	var bundleFileBytes bytes.Buffer
	binary.Write(&bundleFileBytes, binary.LittleEndian, &header)
	// If ChunkCount is 1, we need one chunk size entry
	if header.ChunkCount == 1 {
		binary.Write(&bundleFileBytes, binary.LittleEndian, int32(len(mockIndexData))) // Size of the single chunk
	}
	bundleFileBytes.Write(mockIndexData)

	indexPath, _ := createTempBundleFile(t, bundleFileBytes.Bytes())

	// Use a mock factory
	mockFactory := NewDriveBundleFactory(filepath.Dir(indexPath)) // Or a more specific mock

	idx, err := OpenIndex(indexPath, mockFactory)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}

	if len(idx.Bundles) != numBundles {
		t.Errorf("Expected %d bundles, got %d", numBundles, len(idx.Bundles))
	}
	if len(idx.FilesByPathHash) != numBundles*numFilesPerBundle {
		t.Errorf("Expected %d files, got %d", numBundles*numFilesPerBundle, len(idx.FilesByPathHash))
	}
	if len(idx.Directories) != numDirs {
		t.Errorf("Expected %d directory records, got %d", numDirs, len(idx.Directories))
	}
	if idx.Bundles[0].Path != "Bundle0" {
		t.Errorf("Unexpected path for first bundle: %s", idx.Bundles[0].Path)
	}
}

// TODO: TestIndex_ParsePaths_FNV - Requires carefully crafted DirectoryBundleData and matching file records.
// TODO: TestIndex_ParsePaths_Murmur - Same as above, plus correct MurmurHash.
// TODO: TestIndex_BuildTree - Requires ParsePaths to work and then verifies tree structure.
// TODO: TestBundle_ReadFull_OodleCompressed - Requires a sample Oodle compressed bundle file and working DLL.
//       This test might need to be conditional based on environment capabilities.
//       Example: if oodle.GetDLLPath() == "" { t.Skip("Oodle DLL not found") }
// TODO: Test for bundled GGPK opening (end-to-end for bundledggpk package)


// TestOodleDLL_Acquisition attempts a minimal Oodle call to see if the DLL can be acquired.
func TestOodleDLL_Acquisition(t *testing.T) {
	// This test doesn't validate Oodle's correctness, only if go-oodle can load the library.
	// It attempts to decompress a tiny, potentially invalid, but non-empty buffer.
	// We expect an error, but the type of error will tell us about DLL status.
	dummyCompressed := []byte{0x01, 0x02, 0x03, 0x04} // Arbitrary non-empty
	uncompressedSize := int64(10) // Arbitrary expected size

	_, err := oodle.Decompress(dummyCompressed, uncompressedSize)

	if err != nil {
		// Check for common errors indicating the DLL is missing or couldn't be loaded.
		// Error messages from go-oodle might include these substrings.
		// (Based on typical errors when dynamic libraries are missing)
		missingLibErrors := []string{
			"Could not open Oodle library", // From go-oodle's potential error messages
			"failed to initialize oodle",   // Another potential from go-oodle
			"Dynamic Oodle library not found", // General statement from go-oodle
			"no such file or directory",    // OS error if DLL path is wrong
			"cannot open shared object file", // Linux error
			"image not found",             // macOS error
			// Add more specific error substrings if known from go-oodle
		}
		for _, missingMsg := range missingLibErrors {
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(missingMsg)) {
				t.Skipf("Skipping Oodle functionality tests: Oodle DLL likely not available or failed to load: %v", err)
				return
			}
		}
		// If the error is different, it might be a valid Oodle error (e.g., bad compressed data),
		// which, for this specific test, means the DLL was likely found.
		t.Logf("Oodle Decompress returned an error (as expected with dummy data), but DLL seems present: %v", err)
	} else {
		// Decompress succeeding with dummy data would be highly unexpected but means DLL is present.
		t.Logf("Oodle Decompress succeeded unexpectedly with dummy data (DLL is present).")
	}
}


// Example of how an Oodle test might look (will likely fail if DLL not found by go-oodle)
func TestBundle_ReadFull_OodleCompressed_Leviathan_Example(t *testing.T) {
	t.Skip("Skipping Oodle compressed test: requires actual compressed data and working Oodle DLL.")

	// 1. Create known uncompressed data (e.g., "Hello Oodle")
	// 2. Manually compress it using Oodle Leviathan (e.g., via a C# tool using LibBundle3 or Python script with Oodle bindings)
	//    to get the compressed bytes and the compressed_chunk_sizes array.
	// 3. Construct a BundleHeader for it.
	// 4. Write header, chunk_sizes, and compressed_data to a temp file.
	// 5. Use OpenBundleFile and ReadFull.
	// 6. Compare with original uncompressed data.

	// This is just a conceptual placeholder.
	// Before running, check if Oodle is available:
	_, err := oodle.Decompress([]byte{0x01}, 1) // Minimal check
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not open oodle library") ||
		   strings.Contains(strings.ToLower(err.Error()), "failed to initialize oodle") ||
		   strings.Contains(strings.ToLower(err.Error()), "dynamic oodle library not found") {
			t.Skipf("Skipping Oodle test: Oodle library not available or failed to init: %v", err)
		}
	}
	// ... rest of the test logic using real compressed data ...
}
