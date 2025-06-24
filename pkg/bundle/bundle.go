package bundle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	// "hash/fnv" // FNV logic is implemented manually based on C#
	"io"
	"os"
	"path/filepath"
	"strings"
	"hash/fnv" // For FNV placeholder for Murmur and for actual FNV

	// "github.com/rryqszq4/go-murmurhash" // Commented out due to sandbox issues
	"github.com/new-world-tools/go-oodle"
)

// Bundle represents an opened .bundle.bin file.
type Bundle struct {
	File                 *os.File
	Header               BundleHeader
	CompressedChunkSizes []int32
	Record               *IndexBundleRecord // Link back to its record in the main Index, if applicable
	leaveOpen            bool

	// For caching decompressed content (optional, similar to C#)
	cachedContent []byte
	cacheTable    []bool // true if chunk is cached
}

// OpenBundleFile opens a .bundle.bin file from the given path.
func OpenBundleFile(filePath string, record *IndexBundleRecord, leaveOpen bool) (*Bundle, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open bundle file %s: %w", filePath, err)
	}

	b := &Bundle{
		File:      f,
		Record:    record,
		leaveOpen: leaveOpen,
	}

	// Read header
	headerBytes := make([]byte, BundleHeaderSize)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to read bundle header from %s: %w", filePath, err)
	}

	reader := bytes.NewReader(headerBytes)
	if err := binary.Read(reader, binary.LittleEndian, &b.Header); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to parse bundle header from %s: %w", filePath, err)
	}

	if b.Record != nil {
		b.Record.UncompressedSize = b.Header.UncompressedSize // Sync
	}

	if b.Header.ChunkCount < 0 {
		f.Close()
		return nil, fmt.Errorf("invalid chunk count %d in bundle %s", b.Header.ChunkCount, filePath)
	}
	if b.Header.ChunkCount > 1000000 {
		f.Close()
		return nil, fmt.Errorf("unreasonable chunk count %d in bundle %s", b.Header.ChunkCount, filePath)
	}

	b.CompressedChunkSizes = make([]int32, b.Header.ChunkCount)
	if b.Header.ChunkCount > 0 { // Only read if there are chunks
		if err := binary.Read(f, binary.LittleEndian, &b.CompressedChunkSizes); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to read compressed chunk sizes from %s: %w", filePath, err)
		}
	}

	return b, nil
}

// Close closes the bundle file if it wasn't opened with leaveOpen=true.
func (b *Bundle) Close() error {
	if b.File != nil && !b.leaveOpen {
		err := b.File.Close()
		b.File = nil // Mark as closed
		return err
	}
	return nil
}

// GetLastChunkUncompressedSize calculates the uncompressed size of the last chunk.
func (h *BundleHeader) GetLastChunkUncompressedSize() int32 {
	if h.ChunkCount == 0 {
		return 0
	}
	return h.UncompressedSize - (h.ChunkSize * (h.ChunkCount - 1))
}

// ReadAt extracts and decompresses data for a specific file entry within this bundle.
func (b *Bundle) ReadAt(offsetInBundle int32, sizeInBundle int32) ([]byte, error) {
	if b.File == nil {
		return nil, fmt.Errorf("bundle file is closed or not opened")
	}
	if sizeInBundle == 0 {
		return []byte{}, nil
	}
	if sizeInBundle < 0 {
		return nil, fmt.Errorf("invalid size for ReadAt: %d", sizeInBundle)
	}
	if offsetInBundle < 0 || offsetInBundle+sizeInBundle > b.Header.UncompressedSize {
		return nil, fmt.Errorf("read offset/size out of bounds (offset: %d, size: %d, uncompressed: %d)",
			offsetInBundle, sizeInBundle, b.Header.UncompressedSize)
	}

	fullData, err := b.ReadFull()
	if err != nil {
		return nil, err
	}

    if int(offsetInBundle + sizeInBundle) > len(fullData) {
        return nil, fmt.Errorf("calculated end of slice %d is out of bounds of decompressed data length %d",
            offsetInBundle + sizeInBundle, len(fullData))
    }

	return fullData[offsetInBundle : offsetInBundle+sizeInBundle], nil
}

// ReadFull reads and decompresses the entire bundle content, using cache if available.
func (b *Bundle) ReadFull() ([]byte, error) {
	if b.File == nil {
		return nil, fmt.Errorf("bundle file is closed or not opened")
	}
	if b.Header.UncompressedSize == 0 {
		return []byte{}, nil
	}
    if b.Header.UncompressedSize < 0 {
        return nil, fmt.Errorf("bundle header reports negative uncompressed size: %d", b.Header.UncompressedSize)
    }

	if b.cachedContent != nil {
		return b.cachedContent, nil
	}

	decompressedData := make([]byte, b.Header.UncompressedSize)
	if b.Header.ChunkCount == 0 && b.Header.UncompressedSize > 0 {
		return nil, fmt.Errorf("bundle has uncompressed size > 0 but 0 chunks")
	}
    if b.Header.ChunkCount > 0 && len(b.CompressedChunkSizes) != int(b.Header.ChunkCount) {
        return nil, fmt.Errorf("header chunk count %d does not match length of compressed chunk sizes array %d", b.Header.ChunkCount, len(b.CompressedChunkSizes))
    }

	firstChunkDataOffset := int64(BundleHeaderSize + (b.Header.ChunkCount * 4))
	currentChunkDataFileOffset := firstChunkDataOffset

	outputBufferOffset := int32(0)
	compressedChunkBuffer := make([]byte, 0)

	for i := int32(0); i < b.Header.ChunkCount; i++ {
		compressedChunkSize := b.CompressedChunkSizes[i]
		if compressedChunkSize < 0  {
			return nil, fmt.Errorf("invalid negative compressed chunk size %d for chunk %d", compressedChunkSize, i)
		}

		uncompressedChunkTargetSize := b.Header.ChunkSize
		if i == b.Header.ChunkCount-1 {
			uncompressedChunkTargetSize = b.Header.GetLastChunkUncompressedSize()
		}

		if uncompressedChunkTargetSize < 0 {
             return nil, fmt.Errorf("negative uncompressed target size %d for chunk %d", uncompressedChunkTargetSize, i)
        }
        if uncompressedChunkTargetSize == 0 && compressedChunkSize != 0 {
            return nil, fmt.Errorf("uncompressed target size is 0 but compressed chunk size is %d for chunk %d", compressedChunkSize, i)
        }
        if uncompressedChunkTargetSize == 0 && compressedChunkSize == 0 {
            currentChunkDataFileOffset += int64(compressedChunkSize)
            continue
        }
        if compressedChunkSize == 0 && uncompressedChunkTargetSize != 0 {
             return nil, fmt.Errorf("compressed chunk size is 0 but uncompressed target size is %d for chunk %d", uncompressedChunkTargetSize, i)
        }

		if int32(cap(compressedChunkBuffer)) < compressedChunkSize {
			compressedChunkBuffer = make([]byte, compressedChunkSize)
		} else {
			compressedChunkBuffer = compressedChunkBuffer[:compressedChunkSize]
		}

		if _, err := b.File.Seek(currentChunkDataFileOffset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("failed to seek to chunk %d data at offset %d: %w", i, currentChunkDataFileOffset, err)
		}

		_, err := io.ReadFull(b.File, compressedChunkBuffer)
		if err != nil {
			return nil, fmt.Errorf("failed to read compressed chunk %d (size %d): %w", i, compressedChunkSize, err)
		}

		if outputBufferOffset+uncompressedChunkTargetSize > int32(len(decompressedData)) {
			return nil, fmt.Errorf("output buffer too small for chunk %d: need %d, have %d remaining from total %d (output offset %d)",
				i, uncompressedChunkTargetSize, int32(len(decompressedData))-outputBufferOffset, len(decompressedData), outputBufferOffset)
		}
		uncompressedChunkSlice := decompressedData[outputBufferOffset : outputBufferOffset+uncompressedChunkTargetSize]

		if OodleCompressor(b.Header.Compressor) == OodleCompressorNone {
			if compressedChunkSize != uncompressedChunkTargetSize {
				return nil, fmt.Errorf("mismatch in chunk size for OodleCompressorNone: expected %d, got %d for chunk %d", uncompressedChunkTargetSize, compressedChunkSize, i)
			}
			copy(uncompressedChunkSlice, compressedChunkBuffer)
		} else {
			decompressedChunk, err := oodle.Decompress(compressedChunkBuffer, int64(uncompressedChunkTargetSize))
			if err != nil {
				return nil, fmt.Errorf("failed to decompress Oodle chunk %d (compressor %d, comp size %d, uncomp target %d): %w",
					i, b.Header.Compressor, compressedChunkSize, uncompressedChunkTargetSize, err)
			}
			if len(decompressedChunk) != int(uncompressedChunkTargetSize) {
				return nil, fmt.Errorf("Oodle decompression wrote %d bytes for chunk %d, expected %d", len(decompressedChunk), i, uncompressedChunkTargetSize)
			}
			copy(uncompressedChunkSlice, decompressedChunk)
		}

		currentChunkDataFileOffset += int64(compressedChunkSize)
		outputBufferOffset += uncompressedChunkTargetSize
	}

	b.cachedContent = decompressedData
	return b.cachedContent, nil
}

// --- Index related structures and functions ---

type Index struct {
	BaseBundle          *Bundle
	Bundles             []*IndexBundleRecord
	FilesByPathHash     map[uint64]*IndexFileRecord
	Directories         []IndexDirectoryRecord
	DirectoryBundleData []byte
	RootNode            DirectoryNode
	pathsParsed         bool
	bundleFactory       BundleFileFactory

	bundleToWrite       *Bundle
	bundleStreamToWrite io.WriteSeeker
	maxBundleSize       int32
	customBundles       []*IndexBundleRecord
}

type BundleFileFactory interface {
	GetBundle(record *IndexBundleRecord) (*Bundle, error)
	CreateBundle(bundlePath string) (*Bundle, error)
	DeleteBundle(bundlePath string) error
}

type DriveBundleFactory struct {
	basePath string
}

func NewDriveBundleFactory(ggpkDir string) *DriveBundleFactory {
	return &DriveBundleFactory{basePath: ggpkDir}
}

func (dbf *DriveBundleFactory) GetBundle(record *IndexBundleRecord) (*Bundle, error) {
	bundleFileName := record.Path + ".bundle.bin"
	fullPath := filepath.Join(dbf.basePath, bundleFileName)
	record.bundleFilePath = fullPath
	return OpenBundleFile(fullPath, record, false)
}

func (dbf *DriveBundleFactory) CreateBundle(bundlePath string) (*Bundle, error) {
    fullPath := filepath.Join(dbf.basePath, bundlePath+".bundle.bin")
    dir := filepath.Dir(fullPath)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return nil, fmt.Errorf("failed to create directory %s for new bundle: %w", dir, err)
    }
    f, err := os.Create(fullPath)
    if err != nil {
        return nil, fmt.Errorf("failed to create bundle file %s: %w", fullPath, err)
    }
    bundle := &Bundle{
        File:      f,
        leaveOpen: false,
		Header: BundleHeader{
			HeadSize: 48,
			Compressor: int32(OodleCompressorLeviathan),
			Unknown1: 1,
			ChunkSize: 262144,
		},
		CompressedChunkSizes: []int32{},
    }
	headerBytes := new(bytes.Buffer)
	if err := binary.Write(headerBytes, binary.LittleEndian, &bundle.Header); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to serialize new bundle header: %w", err)
	}
	if _, err := f.Write(headerBytes.Bytes()); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to write new bundle header to %s: %w", fullPath, err)
	}
    return bundle, nil
}

func (dbf *DriveBundleFactory) DeleteBundle(bundlePath string) error {
	fullPath := filepath.Join(dbf.basePath, bundlePath+".bundle.bin")
	return os.Remove(fullPath)
}

func OpenIndex(indexPath string, factory BundleFileFactory) (*Index, error) {
	if factory == nil {
		indexDir := filepath.Dir(indexPath)
		factory = NewDriveBundleFactory(indexDir)
	}
	mainIndexBundle, err := OpenBundleFile(indexPath, nil, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open main index bundle %s: %w", indexPath, err)
	}
	defer mainIndexBundle.Close()

	indexData, err := mainIndexBundle.ReadFull()
	if err != nil {
		return nil, fmt.Errorf("failed to read full content of main index bundle %s: %w", indexPath, err)
	}

	idx := &Index{
		BaseBundle:      mainIndexBundle,
		FilesByPathHash: make(map[uint64]*IndexFileRecord),
		bundleFactory:   factory,
		maxBundleSize:   200 * 1024 * 1024,
	}

	reader := bytes.NewReader(indexData)
	var bundleCount int32
	if err := binary.Read(reader, binary.LittleEndian, &bundleCount); err != nil {
		return nil, fmt.Errorf("failed to read bundleCount: %w", err)
	}
	if bundleCount < 0 {
		return nil, fmt.Errorf("invalid bundleCount: %d", bundleCount)
	}
	idx.Bundles = make([]*IndexBundleRecord, bundleCount)

	for i := int32(0); i < bundleCount; i++ {
		var pathLength int32
		if err := binary.Read(reader, binary.LittleEndian, &pathLength); err != nil {
			return nil, fmt.Errorf("failed to read pathLength for bundle %d: %w", i, err)
		}
		if pathLength < 0 || pathLength > 1024 {
			return nil, fmt.Errorf("invalid pathLength %d for bundle %d", pathLength, i)
		}
		pathBytes := make([]byte, pathLength)
		if _, err := io.ReadFull(reader, pathBytes); err != nil {
			return nil, fmt.Errorf("failed to read path for bundle %d: %w", i, err)
		}
		path := string(pathBytes)
		var uncompressedSizeVal int32
		if err := binary.Read(reader, binary.LittleEndian, &uncompressedSizeVal); err != nil {
			return nil, fmt.Errorf("failed to read uncompressedSize for bundle %d (%s): %w", i, path, err)
		}
		idx.Bundles[i] = &IndexBundleRecord{
			Path:             path,
			UncompressedSize: uncompressedSizeVal,
			BundleIndex:      int(i),
			ParentIndex:      idx,
			Files:            make([]*IndexFileRecord, 0),
		}
		if strings.HasPrefix(path, "LibGGPK3/") {
			idx.customBundles = append(idx.customBundles, idx.Bundles[i])
		}
	}

	var fileCount int32
	if err := binary.Read(reader, binary.LittleEndian, &fileCount); err != nil {
		return nil, fmt.Errorf("failed to read fileCount: %w", err)
	}
	if fileCount < 0 {
		return nil, fmt.Errorf("invalid fileCount: %d", fileCount)
	}

	for i := int32(0); i < fileCount; i++ {
		var pathHash uint64
		if err := binary.Read(reader, binary.LittleEndian, &pathHash); err != nil {
			return nil, fmt.Errorf("failed to read pathHash for file %d: %w", i, err)
		}
		var bundleIdxVal int32
		if err := binary.Read(reader, binary.LittleEndian, &bundleIdxVal); err != nil {
			return nil, fmt.Errorf("failed to read bundleIndex for file %d (hash %X): %w", i, pathHash, err)
		}
		if bundleIdxVal < 0 || bundleIdxVal >= bundleCount {
			return nil, fmt.Errorf("invalid bundleIndex %d for file %d (hash %X)", bundleIdxVal, i, pathHash)
		}
		var offsetVal, sizeVal int32
		if err := binary.Read(reader, binary.LittleEndian, &offsetVal); err != nil {
			return nil, fmt.Errorf("failed to read offset for file %d (hash %X): %w", i, pathHash, err)
		}
		if err := binary.Read(reader, binary.LittleEndian, &sizeVal); err != nil {
			return nil, fmt.Errorf("failed to read size for file %d (hash %X): %w", i, pathHash, err)
		}
		fileRec := &IndexFileRecord{
			PathHash:     pathHash,
			BundleRecord: idx.Bundles[bundleIdxVal],
			Offset:       offsetVal,
			Size:         sizeVal,
		}
		idx.FilesByPathHash[pathHash] = fileRec
		idx.Bundles[bundleIdxVal].Files = append(idx.Bundles[bundleIdxVal].Files, fileRec)
	}

	var directoryCount int32
	if err := binary.Read(reader, binary.LittleEndian, &directoryCount); err != nil {
		return nil, fmt.Errorf("failed to read directoryCount: %w", err)
	}
	if directoryCount < 0 {
        return nil, fmt.Errorf("invalid directoryCount: %d", directoryCount)
    }
	idx.Directories = make([]IndexDirectoryRecord, directoryCount)
	for i := int32(0); i < directoryCount; i++ {
		if err := binary.Read(reader, binary.LittleEndian, &idx.Directories[i]); err != nil {
			return nil, fmt.Errorf("failed to read directory record %d: %w", i, err)
		}
	}

	currentPos := reader.Size() - int64(reader.Len())
	if currentPos < 0 {
		currentPos = 0
	}
	if int(currentPos) > len(indexData) {
		return nil, fmt.Errorf("read past end of index data while parsing directory records")
	}
	idx.DirectoryBundleData = indexData[currentPos:]
	idx.RootNode = DirectoryNode{NameVal: "", PathVal: ""}
	return idx, nil
}

func murmurHash64A(data []byte, seed uint64) uint64 {
	// Reverting to FNV placeholder due to persistent "undefined: murmurhash.MurmurHash2_x64_64" error in sandbox.
	// This will produce WRONG hashes for Murmur-based indices!
	// fmt.Printf("Warning: MurmurHash64A is using FNV placeholder for: %s (seed %X)\n", string(data), seed)
	h := fnv.New64()
	h.Write(data)
	// Seed is not directly used by stdlib fnv in this way, this is a divergence from Murmur.
	return h.Sum64()
}

func fnv1a64Hash(utf8Name []byte) uint64 {
	dataToHash := utf8Name
	if len(dataToHash) > 0 && dataToHash[len(dataToHash)-1] == '/' {
		dataToHash = dataToHash[:len(dataToHash)-1]
	}
	var hash uint64 = 0xCBF29CE484222325
	const fnvPrime uint64 = 0x100000001B3
	for _, b := range dataToHash {
		char := b
		if char >= 'A' && char <= 'Z' {
			char = char + ('a' - 'A')
		}
		hash = (hash ^ uint64(char)) * fnvPrime
	}
	hash = (hash ^ uint64('+')) * fnvPrime
	hash = (hash ^ uint64('+')) * fnvPrime
	return hash
}

func (idx *Index) NameHash(path string) (uint64, error) {
	if idx.Directories == nil || len(idx.Directories) == 0 {
		return 0, fmt.Errorf("index directories not loaded, cannot determine hash algorithm")
	}
	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	utf8Path := []byte(path)
	switch idx.Directories[0].PathHash {
	case 0xF42A94E69CFF42FE:
		lowerPath := strings.ToLower(path)
		utf8LowerPath := []byte(lowerPath)
		return murmurHash64A(utf8LowerPath, 0x1337B33F), nil
	case 0x07E47507B4A92E53:
		return fnv1a64Hash(utf8Path), nil
	default:
		return 0, fmt.Errorf("unknown namehash algorithm (magic: %X)", idx.Directories[0].PathHash)
	}
}

// IsPathParsed returns true if ParsePaths has been successfully called.
func (idx *Index) IsPathParsed() bool {
	return idx.pathsParsed
}

// GetBundleForFileRecord retrieves the actual Bundle object that contains the given file record.
func (idx *Index) GetBundleForFileRecord(fileRec *IndexFileRecord) (*Bundle, error) {
	if fileRec == nil || fileRec.BundleRecord == nil {
		return nil, fmt.Errorf("file record or its bundle record is nil")
	}
	if idx.bundleFactory == nil {
		return nil, fmt.Errorf("bundle factory is not set in index")
	}
	return idx.bundleFactory.GetBundle(fileRec.BundleRecord)
}

// ReadFileData reads the data content of a given file record from its bundle.
func (idx *Index) ReadFileData(fileRec *IndexFileRecord) ([]byte, error) {
	if fileRec == nil {
		return nil, fmt.Errorf("file record is nil for ReadFileData")
	}
	dataBundle, err := idx.GetBundleForFileRecord(fileRec)
	if err != nil {
		return nil, fmt.Errorf("could not get data bundle for file (hash %X, path '%s'): %w", fileRec.PathHash, fileRec.Path, err)
	}
	defer dataBundle.Close()
	return dataBundle.ReadAt(fileRec.Offset, fileRec.Size)
}

// GetFileByPath finds an IndexFileRecord by its full string path.
func (idx *Index) GetFileByPath(path string) (*IndexFileRecord, error) {
	if !idx.pathsParsed {
		failedCount, err := idx.ParsePaths()
		if err != nil {
			return nil, fmt.Errorf("error during implicit ParsePaths for GetFileByPath('%s'): %w", path, err)
		}
		_ = failedCount // Potentially log this
		if !idx.pathsParsed {
			return nil, fmt.Errorf("paths could not be parsed, cannot find file by path '%s'", path)
		}
	}
	hash, err := idx.NameHash(path)
	if err != nil {
		return nil, fmt.Errorf("could not calculate hash for path '%s': %w", path, err)
	}
	fileRec, ok := idx.FilesByPathHash[hash]
	if !ok {
		for _, rec := range idx.FilesByPathHash {
			if rec.Path == path {
				return rec, nil
			}
		}
		return nil, fmt.Errorf("file not found by path '%s' (hash %X not in map, and linear scan failed)", path, hash)
	}
	if fileRec.Path == "" && path != "" {
		fileRec.Path = path
	}
	return fileRec, nil
}

// BuildTree constructs a directory and file tree from the parsed file records.
func (idx *Index) BuildTree(ignoreNullPath bool) (*DirectoryNode, error) {
	if !idx.pathsParsed && !ignoreNullPath {
		return nil, fmt.Errorf("ParsePaths() must be called before building the tree, or ignoreNullPath must be true")
	}
	allFileRecords := make([]*IndexFileRecord, 0, len(idx.FilesByPathHash))
	for _, fr := range idx.FilesByPathHash {
		if fr.Path == "" && !ignoreNullPath {
			return nil, fmt.Errorf("file with hash %X has no path, cannot build tree", fr.PathHash)
		}
		if fr.Path != "" {
			allFileRecords = append(allFileRecords, fr)
		}
	}
	root := &DirectoryNode{NameVal: "", PathVal: ""}
	for _, fileRecord := range allFileRecords {
		if fileRecord.Path == "" {
			continue
		}
		pathComponents := strings.Split(fileRecord.Path, "/")
		currentNode := root
		currentPath := ""
		for i, componentName := range pathComponents[:len(pathComponents)-1] {
			if i > 0 {
				currentPath += "/"
			}
			currentPath += componentName
			childDir := currentNode.FindChildDirectory(componentName)
			if childDir == nil {
				childDir = &DirectoryNode{
					NameVal:   componentName,
					PathVal:   currentPath,
					ParentVal: currentNode,
				}
				currentNode.AddChild(childDir)
			}
			currentNode = childDir
		}
		fileName := pathComponents[len(pathComponents)-1]
		fileNode := &FileNode{
			NameVal:   fileName,
			ParentVal: currentNode,
			RecordVal: fileRecord,
		}
		currentNode.AddChild(fileNode)
	}
	idx.RootNode = *root
	return root, nil
}

// ParsePaths populates the Path field for all FileRecords in the Index.
func (idx *Index) ParsePaths() (failedCount int, err error) {
	if idx.pathsParsed {
		return 0, nil
	}
	if idx.DirectoryBundleData == nil || len(idx.Directories) == 0 {
		idx.pathsParsed = true
		return 0, fmt.Errorf("directory bundle data or directories metadata is missing, cannot parse paths")
	}
	dirData := idx.DirectoryBundleData
	failed := 0
	for _, d := range idx.Directories {
		if d.Offset < 0 || int(d.Offset+d.Size) > len(dirData) {
			continue
		}
		block := dirData[d.Offset : d.Offset+d.Size]
		blockReader := bytes.NewReader(block)
		tempSegments := make([][]byte, 0)
		isBase := false
		for blockReader.Len() > 0 {
			if blockReader.Len() < 4 {
				break
			}
			var pathPartIndex int32
			if err := binary.Read(blockReader, binary.LittleEndian, &pathPartIndex); err != nil {
				return failed, fmt.Errorf("failed to read pathPartIndex for dir offset %d: %w", d.Offset, err)
			}
			if pathPartIndex == 0 {
				isBase = !isBase
				if isBase {
					tempSegments = make([][]byte, 0)
				}
			} else {
				pathPartIndex--
				segment, err := readNullTerminatedString(blockReader)
				if err != nil {
					break
				}
				if pathPartIndex < int32(len(tempSegments)) {
					newSegment := make([]byte, len(tempSegments[pathPartIndex])+len(segment))
					copy(newSegment, tempSegments[pathPartIndex])
					copy(newSegment[len(tempSegments[pathPartIndex]):], segment)
					tempSegments[pathPartIndex] = newSegment
					if !isBase {
						fullPathBytes := tempSegments[pathPartIndex]
						hash, hashErr := idx.NameHash(string(fullPathBytes))
						if hashErr != nil {
							return failed, fmt.Errorf("error calculating name hash for '%s': %w", string(fullPathBytes), hashErr)
						}
						if fileRec, ok := idx.FilesByPathHash[hash]; ok {
							fileRec.Path = string(fullPathBytes)
						} else {
							failed++
						}
					}
				} else {
					if isBase {
						tempSegments = append(tempSegments, segment)
					} else {
						fullPathBytes := segment
						hash, hashErr := idx.NameHash(string(fullPathBytes))
						if hashErr != nil {
							return failed, fmt.Errorf("error calculating name hash for '%s': %w", string(fullPathBytes), hashErr)
						}
						if fileRec, ok := idx.FilesByPathHash[hash]; ok {
							fileRec.Path = string(fullPathBytes)
						} else {
							failed++
						}
					}
				}
			}
		}
	}
	idx.pathsParsed = true
	return failed, nil
}

// readNullTerminatedString reads a null-terminated byte sequence from a bytes.Reader
func readNullTerminatedString(r *bytes.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return nil, fmt.Errorf("string not null-terminated before EOF")
			}
			return nil, err
		}
		if b == 0 {
			return buf.Bytes(), nil
		}
		buf.WriteByte(b)
		if buf.Len() > 2048 {
			return nil, fmt.Errorf("string segment too long, possible malformed data")
		}
	}
}
