package main

import (
	"bufio"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit("")
	}

	switch os.Args[1] {
	case "init":
		gitInit()
	case "cat-file":
		gitCatFile()
	case "hash-object":
		gitHashObject()
	default:
		fmt.Printf("invalid command: %s\n", os.Args[1])
		printUsageAndExit("")
	}
}

func printUsageAndExit(command string) {
	myName := filepath.Base(os.Args[0])
	if len(command) == 0 {
		fmt.Printf("usage: %s <command> [<args>...]", myName)
	} else {
		fmt.Printf("usage: %s %s", myName, command)
	}
	os.Exit(1)
}

func fatal(msg string, args ...any) {
	fmt.Printf(msg, args...)
	os.Exit(128)
}

func gitInit() {
	initialDirectories := []string{".git", ".git/objects", ".git/refs"}
	for _, directory := range initialDirectories {
		err := os.Mkdir(directory, 0755)
		if err != nil {
			fatal("error creating directory %s: %s", directory, err)
		}
	}
	headPath := ".git/HEAD"
	headContent := "ref: refs/heads/master\n"
	err := os.WriteFile(headPath, []byte(headContent), 0644)
	if err != nil {
		fatal("error writing to file %s: %s", headPath, err)
	}
	cwd, _ := os.Getwd()
	repository := filepath.Join(cwd, ".git")
	fmt.Printf("Initialized empty Git repository in %s\n", repository)
}

func gitCatFile() {
	if len(os.Args) < 4 || !(os.Args[2] == "-p" || os.Args[2] == "-t" || os.Args[2] == "-s" || os.Args[2] == "-e") {
		printUsageAndExit("cat-file (-p | -t | -s | -e) <object>")
	}

	objName := os.Args[3]
	if len(objName) != 40 {
		fatal("fatal: Not a valid object name %s\n", objName)
	}

	objDir := filepath.Join(".git", "objects", objName[:2])
	info, err := os.Stat(objDir)
	if err != nil {
		fatal(err.Error())
	}
	if !info.IsDir() {
		fatal("fatal: not a directory %s\n", objDir)
	}

	objPath := filepath.Join(objDir, objName[2:])
	file, err := os.Open(objPath)
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	if os.Args[2] == "-e" { // only check if object exists
		os.Exit(0)
	}

	zipReader, err := zlib.NewReader(file)
	if err != nil {
		fatal(err.Error())
	}

	reader := bufio.NewReader(zipReader)
	objType, _ := reader.ReadString(' ')
	objType = objType[:len(objType)-1]

	if os.Args[2] == "-t" {
		fmt.Println(objType)
		return
	}

	if objType != "blob" {
		fatal("object type not supported: %q\n", objType)
	}

	lengthStr, err := reader.ReadString(0)
	lengthStr = lengthStr[:len(lengthStr)-1]
	if err != nil {
		fatal(err.Error())
	}
	objSize, _ := strconv.ParseInt(lengthStr, 10, 64)

	if os.Args[2] == "-s" {
		fmt.Println(objSize)
		return
	}

	if objSize == 0 {
		fatal("error: object file %s is empty", objPath)
	}

	buffer := make([]byte, objSize)
	_, err = reader.Read(buffer)
	if err != nil {
		fatal(err.Error())
	}

	// default action "-p" (pretty-print)

	os.Stdout.Write(buffer)
}

func gitHashObject() {
	if len(os.Args) < 3 || (os.Args[2] == "-w" && len(os.Args) < 4) {
		printUsageAndExit("hash-object -w <object>")
	}

	var writeObject bool
	var filename string
	if os.Args[2] == "-w" {
		filename = os.Args[3]
		writeObject = true
	} else {
		filename = os.Args[2]
	}

	file, err := os.Open(filename)
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		fatal(err.Error())
	}
	if info.IsDir() {
		fatal("'%s' is a directory", info.Name())
	}
	fileSize := info.Size()

	payload := []byte(fmt.Sprintf("blob %d\000", fileSize))
	content := make([]byte, fileSize)
	_, err = file.Read(content)
	if err != nil {
		fatal(err.Error())
	}

	s := sha1.New()
	s.Write(payload)
	s.Write(content)

	objName := fmt.Sprintf("%x", s.Sum(nil))

	if !writeObject {
		fmt.Println(objName)
		return
	}

	objDir := objName[:2]
	err = os.MkdirAll(filepath.Join(".git", "objects", objDir), 0755)
	if err != nil {
		fatal(err.Error())
	}

	objFile, err := os.OpenFile(filepath.Join(".git", "objects", objDir, objName[2:]), os.O_CREATE, 0644)
	if err != nil {
		fatal(err.Error())
	}
	defer objFile.Close()
	writer := zlib.NewWriter(objFile)
	writer.Write(payload)
	writer.Write(content)
	err = writer.Close()
	if err != nil {
		fatal(err.Error())
	}

	fmt.Println(objName)
}
