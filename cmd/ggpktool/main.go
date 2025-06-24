package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings" // Added import

	"github.com/user/ggpkgo/pkg/ggpk"
)

func main() {
	ggpkPath := flag.String("ggpk", "", "Path to the GGPK file (required)")
	action := flag.String("action", "list", "Action to perform: list, extract, extract-all")
	itemPath := flag.String("path", "", "Path of the item within GGPK to extract")
	outputPath := flag.String("out", ".", "Output directory for extracted files/all files")

	flag.Parse()

	if *ggpkPath == "" {
		fmt.Println("Error: -ggpk flag is required")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("GGPK Tool - Go Version\n")
	fmt.Printf("Processing GGPK file: %s\n", *ggpkPath)
	fmt.Printf("Action: %s\n", *action)

	// Open the GGPK file
	gf, err := ggpk.Open(*ggpkPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening GGPK file %s: %v\n", *ggpkPath, err)
		os.Exit(1)
	}
	defer gf.Close()

	switch *action {
	case "list":
		// Pass gf to listContents
		err = listContents(gf, gf.Root, "", 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing contents: %v\n", err)
			os.Exit(1)
		}
	case "extract":
		if *itemPath == "" {
			fmt.Println("Error: -path flag is required for 'extract' action")
			os.Exit(1)
		}
		// Ensure output path is a directory, use itemPath's base name for the file
		outFilePath := filepath.Join(*outputPath, filepath.Base(*itemPath))
		err = extractFile(gf, *itemPath, outFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting file '%s': %v\n", *itemPath, err)
			os.Exit(1)
		}
		fmt.Printf("File '%s' extracted to '%s'\n", *itemPath, outFilePath)
	case "extract-all":
		fmt.Println("Extracting all files...")
		err = extractAllFiles(gf, gf.Root, *outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during extract-all: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("All files extracted to:", *outputPath)
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown action '%s'\n", *action)
		flag.Usage()
		os.Exit(1)
	}
}

// listContents is the initial entry point for listing.
// It calls listContentsRecursive.
func listContents(ggpkFile *ggpk.GGPKFile, node ggpk.TreeNode, currentPath string, depth int) error {
	// Print root separately if it's the initial call
	if depth == 0 && node == ggpkFile.Root {
		fmt.Printf("/\n")
	}
	return listContentsRecursive(node, currentPath, depth, ggpkFile)
}


// listContentsRecursive recursively lists the contents of a directory node.
func listContentsRecursive(node ggpk.TreeNode, parentPath string, depth int, ggpkFile *ggpk.GGPKFile) error {
	if node == nil {
		return nil
	}
	indent := strings.Repeat("  ", depth)
	nodeName := node.GetName()

	// For root, GetPath() is "", name is "".
	// For children of root, GetPath() is "Name", name is "Name".
	// For deeper children, GetPath() is "Parent/Name", name is "Name".
	// The `parentPath` argument to listContentsRecursive should be the *parent's* full path.
	// The node's own GetPath() gives its full path.

	// Only print nodeName if it's not the root being specially handled by listContents
	if !(depth == 0 && nodeName == "" && parentPath == "") {
		fmt.Printf("%s%s\n", indent, nodeName)
	}


	if dirNode, ok := node.(*ggpk.DirectoryRecord); ok {
		// Ensure children are loaded for this directory node
		children, err := dirNode.GetChildren(ggpkFile)
		if err != nil {
			// Log this error but try to continue if possible
			fmt.Fprintf(os.Stderr, "Error getting children for %s: %v\n", node.GetPath(), err)
			return nil // Or return err to stop all listing
		}
		for _, child := range children {
			// Construct the new parentPath for the recursive call
			childParentPath := node.GetPath()
			if err := listContentsRecursive(child, childParentPath, depth+1, ggpkFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing child of %s: %v\n", node.GetPath(), err)
				// Decide whether to continue or bubble up error
			}
		}
	}
	return nil
}


// extractFile extracts a single file from GGPK to the specified output path.
func extractFile(gf *ggpk.GGPKFile, itemPath string, outFilePath string) error {
	fmt.Printf("Extracting '%s' to '%s'\n", itemPath, outFilePath)
	node, err := gf.GetNodeByPath(itemPath)
	if err != nil {
		return err
	}

	fileNode, ok := node.(*ggpk.FileRecord)
	if !ok {
		return fmt.Errorf("path '%s' is not a file", itemPath)
	}

	fileData, err := gf.ReadFileData(fileNode)
	if err != nil {
		return fmt.Errorf("failed to read file data for '%s': %w", itemPath, err)
	}

	// Ensure output directory exists
	outDir := filepath.Dir(outFilePath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory '%s': %w", outDir, err)
	}

	err = os.WriteFile(outFilePath, fileData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write extracted file to '%s': %w", outFilePath, err)
	}
	return nil
}

// extractAllFiles recursively extracts all files from a directory node.
func extractAllFiles(gf *ggpk.GGPKFile, node ggpk.TreeNode, baseOutputDir string) error {
	if node == nil {
		return nil
	}

	nodePath := node.GetPath() // This gives the full path from GGPK root

	if fileNode, ok := node.(*ggpk.FileRecord); ok {
		// Construct output path, maintaining directory structure
		// nodePath is like "Data/Items.dat" or "RootFile.txt"
		// We want to join it with baseOutputDir
		outFilePath := filepath.Join(baseOutputDir, filepath.FromSlash(nodePath))

		fmt.Printf("Extracting %s -> %s\n", nodePath, outFilePath)

		// Ensure directory for the file exists
		outDir := filepath.Dir(outFilePath)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s for file %s: %w", outDir, nodePath, err)
		}

		fileData, err := gf.ReadFileData(fileNode)
		if err != nil {
			// Log error and continue? For extract-all, maybe skip problematic files.
			fmt.Fprintf(os.Stderr, "Error reading data for %s: %v. Skipping.\n", nodePath, err)
			return nil // Continue with other files
		}
		err = os.WriteFile(outFilePath, fileData, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file %s to %s: %v. Skipping.\n", nodePath, outFilePath, err)
			return nil // Continue with other files
		}
	} else if dirNode, ok := node.(*ggpk.DirectoryRecord); ok {
		// If it's the root node and its path is "", we don't want to create a "" folder.
		// Children's paths will be relative to this.
		// For non-root directories, ensure the directory exists in the output.
		if nodePath != "" { // Root node's GetPath() might be ""
			currentOutDir := filepath.Join(baseOutputDir, filepath.FromSlash(nodePath))
			if err := os.MkdirAll(currentOutDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", currentOutDir, err)
			}
		}

		children, err := dirNode.GetChildren(gf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting children for %s: %v. Skipping directory.\n", nodePath, err)
			return nil // Continue with other parts
		}
		for _, child := range children {
			if err := extractAllFiles(gf, child, baseOutputDir); err != nil {
				// If a recursive call fails hard, we might want to propagate it.
				// For now, individual file errors are logged and skipped.
				// This error here might be for directory creation.
				return err
			}
		}
	}
	return nil
}
