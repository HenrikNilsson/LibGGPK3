package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	// "strings" // No longer used directly

	"github.com/user/ggpkgo/pkg/bundle"
	"github.com/user/ggpkgo/pkg/bundledggpk"
	"github.com/user/ggpkgo/pkg/ggpk"
)

// Copied from cmd/ggpktool/main.go - consider refactoring to a shared utility package if more tools are made.
func listContentsRecursiveSimple(node ggpk.TreeNode, currentIndent string, ggpkFile *ggpk.GGPKFile) error {
	if node == nil {
		return nil
	}
	fmt.Printf("%s%s\n", currentIndent, node.GetName())

	if dirNode, ok := node.(*ggpk.DirectoryRecord); ok {
		children, err := dirNode.GetChildren(ggpkFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting children for %s: %v\n", node.GetPath(), err)
			return nil
		}
		for _, child := range children {
			if err := listContentsRecursiveSimple(child, currentIndent+"  ", ggpkFile); err != nil {
				// Log error or decide to bubble up
				fmt.Fprintf(os.Stderr, "Error processing child of %s: %v\n", node.GetPath(), err)
			}
		}
	}
	return nil
}


func main() {
	indexBinPath := flag.String("index", "", "Path to the _.index.bin file (required)")
	ggpkInBundlePath := flag.String("ggpkpath", "", "Path of the GGPK file within the bundle system (e.g., Bundles2/Content.ggpk or _.ggpk) (required)")
	action := flag.String("action", "list", "Action: list, extract")
	itemPath := flag.String("itempath", "", "Path of the item within the bundled GGPK to extract (for action=extract)")
	outputPath := flag.String("out", ".", "Output directory for extracted file (for action=extract)")

	flag.Parse()

	if *indexBinPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -index flag (path to _.index.bin) is required.")
		flag.Usage()
		os.Exit(1)
	}
	if *ggpkInBundlePath == "" {
		fmt.Fprintln(os.Stderr, "Error: -ggpkpath flag (path of GGPK within bundle) is required.")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Extract Bundled GGPK Tool\n")
	fmt.Printf("Processing index: %s\n", *indexBinPath)
	fmt.Printf("GGPK path in bundle: %s\n", *ggpkInBundlePath)
	fmt.Printf("Action: %s\n", *action)

	// Determine the base directory for DriveBundleFactory (directory of the index file)
	indexDir := filepath.Dir(*indexBinPath)
	bundleFactory := bundle.NewDriveBundleFactory(indexDir)

	// 1. Open the main bundle index
	fmt.Println("Opening bundle index...")
	idx, err := bundle.OpenIndex(*indexBinPath, bundleFactory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening bundle index %s: %v\n", *indexBinPath, err)
		os.Exit(1)
	}
	// Note: ParsePaths might be implicitly called by GetFileByPath if needed.
	// Or explicitly:
	// if !idx.IsPathParsed() {
	//    fmt.Println("Parsing paths in index...")
	//	  if _, err := idx.ParsePaths(); err != nil {
	//		  fmt.Fprintf(os.Stderr, "Error parsing bundle index paths: %v\n", err)
	//		  os.Exit(1)
	//	  }
	// }


	// 2. Open the bundled GGPK
	fmt.Printf("Opening bundled GGPK '%s'...\n", *ggpkInBundlePath)
	bundledGGPKFile, err := bundledggpk.OpenBundledGGPK(idx, *ggpkInBundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening bundled GGPK '%s': %v\n", *ggpkInBundlePath, err)
		os.Exit(1)
	}
	defer bundledGGPKFile.Close() // This will be a no-op as it's an in-memory GGPK now

	fmt.Printf("Successfully opened bundled GGPK: %s\n", *ggpkInBundlePath)

	// 3. Perform action on the bundled GGPK
	switch *action {
	case "list":
		fmt.Println("Contents of bundled GGPK:")
		// Use a simplified listing function (can be refactored from ggpktool if complex)
		if err := listContentsRecursiveSimple(bundledGGPKFile.Root, "", bundledGGPKFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing contents of bundled GGPK: %v\n", err)
			os.Exit(1)
		}
	case "extract":
		if *itemPath == "" {
			fmt.Fprintln(os.Stderr, "Error: -itempath flag is required for 'extract' action.")
			os.Exit(1)
		}

		outFileName := filepath.Base(*itemPath)
		outFilePath := filepath.Join(*outputPath, outFileName)

		fmt.Printf("Extracting '%s' from bundled GGPK to '%s'...\n", *itemPath, outFilePath)

		node, err := bundledGGPKFile.GetNodeByPath(*itemPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding item '%s' in bundled GGPK: %v\n", *itemPath, err)
			os.Exit(1)
		}

		fileNode, ok := node.(*ggpk.FileRecord)
		if !ok {
			fmt.Fprintf(os.Stderr, "Item '%s' in bundled GGPK is not a file.\n", *itemPath)
			os.Exit(1)
		}

		fileData, err := bundledGGPKFile.ReadFileData(fileNode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading data for item '%s' from bundled GGPK: %v\n", *itemPath, err)
			os.Exit(1)
		}

		outDir := filepath.Dir(outFilePath)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output directory '%s': %v\n", outDir, err)
			os.Exit(1)
		}

		if err := os.WriteFile(outFilePath, fileData, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing extracted file to '%s': %v\n", outFilePath, err)
			os.Exit(1)
		}
		fmt.Printf("Successfully extracted '%s' to '%s'\n", *itemPath, outFilePath)

	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown action '%s'. Supported actions: list, extract.\n", *action)
		flag.Usage()
		os.Exit(1)
	}
}
