package bundle

// BundleHeader corresponds to Bundle.Header in C# (60 bytes)
// It's found at the beginning of each .bundle.bin file.
type BundleHeader struct {
	UncompressedSize    int32
	CompressedSize      int32
	HeadSize            int32 // chunk_count * 4 + 48 (effectively offset to first chunk data)
	Compressor          int32 // Oodle.Compressor enum value, e.g., Leviathan = 13
	Unknown1            int32 // Defaults to 1
	UncompressedSizeLong int64 // Should match UncompressedSize
	CompressedSizeLong  int64 // Should match CompressedSize
	ChunkCount          int32
	ChunkSize           int32 // Default 256KB = 262144
	Unknown3            int32 // Defaults to 0
	Unknown4            int32 // Defaults to 0
	Unknown5            int32 // Defaults to 0
	Unknown6            int32 // Defaults to 0
}

const BundleHeaderSize = 60

// OodleCompressor mirrors the Oodle.Compressor enum
type OodleCompressor int32

const (
	OodleCompressorInvalid    OodleCompressor = -1
	OodleCompressorNone       OodleCompressor = 3
	OodleCompressorKraken     OodleCompressor = 8
	OodleCompressorLeviathan  OodleCompressor = 13
	OodleCompressorMermaid    OodleCompressor = 9
	OodleCompressorSelkie     OodleCompressor = 11
	OodleCompressorHydra      OodleCompressor = 12
	// Deprecated ones omitted for now
)

// IndexBundleRecord corresponds to LibBundle3.Records.BundleRecord
// This represents a data bundle file listed within the main index.
type IndexBundleRecord struct {
	Path             string // Path of the bundle file (e.g., "Art/Models.bundle.bin"), without ".bundle.bin" in index storage
	UncompressedSize int32  // Total uncompressed size of this data bundle
	BundleIndex      int    // Its index in the main Index's list of bundles
	ParentIndex      *Index // Reference to the parent Index object (conceptual)
	Files            []*IndexFileRecord // Files contained in this bundle

	// Non-serialized fields for runtime
	bundleFilePath string // Full path to the .bundle.bin file
}

// IndexFileRecord corresponds to LibBundle3.Records.FileRecord
// This represents a single file's metadata within the main index.
type IndexFileRecord struct {
	PathHash     uint64 // Hash of the file's full path
	BundleRecord *IndexBundleRecord // Which bundle contains this file
	Offset       int32  // Offset of the file content within its bundle's decompressed data
	Size         int32  // Size of the file content (uncompressed)
	Path         string // Full path of the file (e.g., "Art/Textures/MyTexture.dds"), populated by Index.ParsePaths()
}

// IndexDirectoryRecord corresponds to Index.DirectoryRecord in C#
// This seems to be metadata about directory paths used by ParsePaths.
type IndexDirectoryRecord struct {
	PathHash      uint64
	Offset        int32 // Offset within the directoryBundleData
	Size          int32 // Size of the path component data at Offset
	RecursiveSize int32 // Unclear, possibly total size of all path data under this entry
}

// TreeNode interface for bundle file/directory representation (similar to GGPK's)
type TreeNode interface {
	GetName() string
	GetPath() string
	IsDirectory() bool
	GetParent() *DirectoryNode // Changed to pointer to allow nil for root
}

// FileNode represents a file in the bundle's conceptual tree structure.
type FileNode struct {
	NameVal   string
	ParentVal *DirectoryNode
	RecordVal *IndexFileRecord
}

func (fn *FileNode) GetName() string       { return fn.NameVal }
func (fn *FileNode) GetPath() string       {
	if fn.RecordVal != nil && fn.RecordVal.Path != "" {
		return fn.RecordVal.Path
	}
	// Fallback if path isn't populated in record, construct from parent
	if fn.ParentVal == nil || fn.ParentVal.GetPath() == "" { // Parent is root
		return fn.NameVal
	}
	return fn.ParentVal.GetPath() + "/" + fn.NameVal
}
func (fn *FileNode) IsDirectory() bool     { return false }
func (fn *FileNode) GetParent() *DirectoryNode { return fn.ParentVal }


// DirectoryNode represents a directory in the bundle's conceptual tree structure.
type DirectoryNode struct {
	NameVal     string
	PathVal     string
	ParentVal   *DirectoryNode // Parent can be nil for root
	ChildrenVal []TreeNode
}

func (dn *DirectoryNode) GetName() string       { return dn.NameVal }
func (dn *DirectoryNode) GetPath() string       { return dn.PathVal }
func (dn *DirectoryNode) IsDirectory() bool     { return true }
func (dn *DirectoryNode) GetParent() *DirectoryNode { return dn.ParentVal }

func (dn *DirectoryNode) AddChild(child TreeNode) {
	dn.ChildrenVal = append(dn.ChildrenVal, child)
}

// Helper to find a child directory by name
func (dn *DirectoryNode) FindChildDirectory(name string) *DirectoryNode {
	for _, child := range dn.ChildrenVal {
		if child.IsDirectory() && child.GetName() == name {
			// Type assertion, ensure it's safe or handle error
			if dirChild, ok := child.(*DirectoryNode); ok {
				return dirChild
			}
		}
	}
	return nil
}


// Ensure types implement TreeNode (compile-time check)
var _ TreeNode = (*FileNode)(nil)
var _ TreeNode = (*DirectoryNode)(nil)
