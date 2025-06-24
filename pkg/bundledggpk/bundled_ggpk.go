package bundledggpk

import (
	"bytes"
	"fmt"

	"github.com/user/ggpkgo/pkg/bundle" // Assuming this is the module path
	"github.com/user/ggpkgo/pkg/ggpk"
)

// OpenBundledGGPK finds a GGPK file within a bundle index, extracts its data,
// and then opens it using the ggpk package's OpenFromReader.
func OpenBundledGGPK(idx *bundle.Index, pathInBundle string) (*ggpk.GGPKFile, error) {
	if idx == nil {
		return nil, fmt.Errorf("bundle index is nil")
	}
	if pathInBundle == "" {
		return nil, fmt.Errorf("pathInBundle cannot be empty")
	}

	// 1. Find the file record for the GGPK file in the bundle index.
	// For this, Index needs a way to get a file record by its path.
	// This would typically involve NameHash and then lookup, or direct path lookup if tree is built.
	// Let's assume Index has a method `GetFileRecordByPath(path string)` or similar.
	// For now, we'll iterate through FilesByPathHash if ParsePaths has been called.
	// This part needs a robust way to get the IndexFileRecord.

	// Ensure paths are parsed in the index so we can iterate by string path.
	// In a real scenario, the caller of OpenBundledGGPK might need to ensure this,
	// or OpenBundledGGPK could trigger it.
	if !idx.IsPathParsed() { // Assuming IsPathParsed() method exists
		_, err := idx.ParsePaths() // Assuming ParsePaths() returns (int, error)
		if err != nil {
			return nil, fmt.Errorf("failed to parse paths in bundle index: %w", err)
		}
	}

	var ggpkFileRecord *bundle.IndexFileRecord
	// Iterate over files to find by path - this is inefficient.
	// A proper Index.GetFileByPath(string) method would be better.
	// This assumes pathInBundle is exactly how it's stored after ParsePaths.
	for _, fileRec := range idx.FilesByPathHash {
		if fileRec.Path == pathInBundle {
			ggpkFileRecord = fileRec
			break
		}
	}

	if ggpkFileRecord == nil {
		return nil, fmt.Errorf("GGPK file '%s' not found in bundle index", pathInBundle)
	}

	// 2. Extract the byte content of the GGPK file from its bundle.
	// Bundle IndexFileRecord has BundleRecord, Offset, and Size.
	// BundleRecord needs to provide a way to get its actual Bundle object.
	// Then Bundle object needs ReadAt(offset, size).
	// The C# `FileRecord.Read(Bundle? bundle = null)` implies if you don't pass a bundle, it gets one.
	// We use idx.ReadFileData(ggpkFileRecord) which handles getting and closing the bundle.

	ggpkFileBytes, err := idx.ReadFileData(ggpkFileRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to read data for GGPK file '%s' from bundle: %w", pathInBundle, err)
	}

	if len(ggpkFileBytes) == 0 {
		return nil, fmt.Errorf("extracted GGPK file '%s' has no content", pathInBundle)
	}

	// 3. Use ggpk.OpenFromReader to parse these bytes.
	ggpkReader := bytes.NewReader(ggpkFileBytes)
	fileSize := int64(len(ggpkFileBytes))

	parsedGGPK, err := ggpk.OpenFromReader(ggpkReader, fileSize)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bundled GGPK file '%s': %w", pathInBundle, err)
	}

	return parsedGGPK, nil
}

// Helper methods that would be needed in pkg/bundle/bundle.go (Index struct):
// - func (idx *Index) IsPathParsed() bool { return idx.pathsParsed }
// - func (idx *Index) GetBundleForFileRecord(fileRec *IndexFileRecord) (*Bundle, error) {
//     return idx.bundleFactory.GetBundle(fileRec.BundleRecord)
//   }
// - func (idx *Index) ReadFileData(fileRec *IndexFileRecord) ([]byte, error) {
//	   bundle, err := idx.GetBundleForFileRecord(fileRec)
//	   if err != nil { return nil, err }
//     defer bundle.Close() // Important if GetBundleForFileRecord opens a new one each time
//	   return bundle.ReadAt(fileRec.Offset, fileRec.Size)
//   }
// Note: The deferred close for `bundle` in `ReadFileData` assumes `GetBundleForFileRecord`
// returns a fresh bundle instance that needs closing. If the factory/index caches and manages
// bundle instances, then closing it here might be wrong. This needs careful design in the bundle package.
// For now, this structure provides the idea.
