package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit()
	}

	switch os.Args[1] {
	case "init":
		gitInit()
	default:
		fmt.Printf("invalid command: %s\n", os.Args[1])
		printUsageAndExit()
	}
}

func printUsageAndExit() {
	fmt.Printf("usage: %s <command> [<args>...]", filepath.Base(os.Args[0]))
	os.Exit(1)
}

func gitInit() {
	initialDirectories := []string{".git", ".git/objects", ".git/refs"}
	for _, directory := range initialDirectories {
		err := os.Mkdir(directory, 0755)
		if err != nil {
			fmt.Printf("error creating directory %s: %s", directory, err)
			os.Exit(1)
		}
	}
	headPath := ".git/HEAD"
	headContent := "ref: refs/heads/master\n"
	err := os.WriteFile(headPath, []byte(headContent), 0644)
	if err != nil {
		fmt.Printf("error writing to file %s: %s", headPath, err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	repository := filepath.Join(cwd, ".git")
	fmt.Printf("Initialized empty Git repository in %s\n", repository)
}
