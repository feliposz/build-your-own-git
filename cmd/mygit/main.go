package main

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
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
	case "ls-tree":
		gitListTree()
	case "write-tree":
		gitWriteTree()
	case "commit-tree":
		gitCommitTree()
	case "clone":
		gitClone()
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

	// default action "-p" (pretty-print)

	io.Copy(os.Stdout, reader)
}

func gitHashObject() {
	if len(os.Args) < 3 || (os.Args[2] == "-w" && len(os.Args) < 4) {
		printUsageAndExit("hash-object [-w] <object>")
	}

	var writeObject bool
	var filename string
	if os.Args[2] == "-w" {
		filename = os.Args[3]
		writeObject = true
	} else {
		filename = os.Args[2]
	}

	fmt.Printf("%x\n", hashFile(writeObject, filename))
}

func hashFile(writeObject bool, filename string) []byte {
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

	content := make([]byte, fileSize)
	_, err = file.Read(content)
	if err != nil {
		fatal(err.Error())
	}

	return hashObject(writeObject, "blob", fileSize, content)
}

func hashObject(writeObject bool, contentType string, contentSize int64, content []byte) []byte {
	payload := []byte(fmt.Sprintf("%s %d\000", contentType, contentSize))

	s := sha1.New()
	s.Write(payload)
	s.Write(content)

	hash := s.Sum(nil)
	objName := fmt.Sprintf("%x", hash)

	if !writeObject {
		return hash
	}

	objDir := filepath.Join(".git", "objects", objName[:2])
	objPath := filepath.Join(objDir, objName[2:])

	// no need to rewrite if contents match (same hash)
	if fileExists(objPath) {
		return hash
	}

	err := os.MkdirAll(objDir, 0755)
	if err != nil {
		fatal(err.Error())
	}

	objFile, err := os.OpenFile(objPath, os.O_CREATE, 0644)
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

	return hash
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		fatal(err.Error())
	}
	return true
}

func gitListTree() {
	if len(os.Args) < 3 || (len(os.Args) == 4 && os.Args[2] != "--name-only" && os.Args[2] != "--object-only" && os.Args[2] != "-l") {
		printUsageAndExit("ls-tree [(-l | --name-only | --object-only)] <tree_sha>")
	}

	var nameOnly, objectOnly, longFormat bool
	var objName string
	if os.Args[2] == "--name-only" {
		objName = os.Args[3]
		nameOnly = true
	} else if os.Args[2] == "--object-only" {
		objName = os.Args[3]
		objectOnly = true
	} else if os.Args[2] == "-l" {
		objName = os.Args[3]
		longFormat = true
	} else {
		objName = os.Args[2]
	}

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

	zipReader, err := zlib.NewReader(file)
	if err != nil {
		fatal(err.Error())
	}

	reader := bufio.NewReader(zipReader)
	objType, _ := reader.ReadString(' ')
	objType = objType[:len(objType)-1]

	if objType != "tree" {
		fatal("expected a 'tree' node, found: %q\n", objType)
	}

	lengthStr, err := reader.ReadString(0)
	lengthStr = lengthStr[:len(lengthStr)-1]
	if err != nil {
		fatal(err.Error())
	}
	objSize, _ := strconv.ParseInt(lengthStr, 10, 64)

	if objSize == 0 {
		fatal("error: object file %s is empty", objPath)
	}

	hash := make([]byte, 20)
	for {
		fileMode, err := reader.ReadString(' ')
		if err != nil {
			if err == io.EOF {
				break
			}
			fatal(err.Error())
		}
		fileMode = "000000" + fileMode[:len(fileMode)-1]
		fileMode = fileMode[len(fileMode)-6:]

		name, err := reader.ReadString('\000')
		if err != nil {
			fatal(err.Error())
		}
		name = name[:len(name)-1]

		_, err = reader.Read(hash)
		if err != nil {
			fatal(err.Error())
		}

		if nameOnly {
			fmt.Println(name)
		} else if objectOnly {
			fmt.Printf("%x\n", hash)
		} else if longFormat {
			objType, objSize := getObjTypeAndSize(fmt.Sprintf("%x", hash))
			fmt.Printf("%s %s %x\t%d\t%s\n", fileMode, objType, hash, objSize, name)
		} else {
			objType, _ := getObjTypeAndSize(fmt.Sprintf("%x", hash))
			fmt.Printf("%s %s %x\t%s\n", fileMode, objType, hash, name)
		}
	}
}

func getObjTypeAndSize(objName string) (objType string, objSize int64) {
	objPath := filepath.Join(".git", "objects", objName[:2], objName[2:])

	file, err := os.Open(objPath)
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	zipReader, err := zlib.NewReader(file)
	if err != nil {
		fatal(err.Error())
	}

	reader := bufio.NewReader(zipReader)
	objType, _ = reader.ReadString(' ')
	objType = objType[:len(objType)-1]

	lengthStr, _ := reader.ReadString(0)
	lengthStr = lengthStr[:len(lengthStr)-1]
	objSize, _ = strconv.ParseInt(lengthStr, 10, 64)

	return
}

func gitWriteTree() {
	if len(os.Args) != 2 {
		printUsageAndExit("write-tree")
	}

	cwd, _ := os.Getwd()
	fmt.Printf("%x\n", writeTree(cwd))
}

type treeEntry struct {
	name string
	mode string
	hash []byte
}

func writeTree(path string) []byte {
	dir, err := os.Open(path)
	if err != nil {
		fatal(err.Error())
	}
	defer dir.Close()

	if info, _ := dir.Stat(); !info.IsDir() {
		fatal("not a directory: %s\n", path)
	}

	entries, err := dir.ReadDir(0)
	if err != nil {
		fatal(err.Error())
	}

	treeEntries := []*treeEntry{}

	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		te := new(treeEntry)
		te.name = entry.Name()
		fullPath := filepath.Join(path, te.name)
		if entry.IsDir() {
			te.mode = "40000" // directory
			te.hash = writeTree(fullPath)
		} else {
			// 100755 (executable file)
			// 120000 (symbolic link)
			te.mode = "100644" // regular file
			te.hash = hashFile(true, fullPath)
		}
		treeEntries = append(treeEntries, te)
	}

	slices.SortFunc(treeEntries, func(a, b *treeEntry) int {
		return cmp.Compare(a.name, b.name)
	})

	content := []byte{}
	for _, entry := range treeEntries {
		content = append(content, []byte(entry.mode)...)
		content = append(content, ' ')
		content = append(content, []byte(entry.name)...)
		content = append(content, '\000')
		content = append(content, entry.hash...)
	}

	return hashObject(true, "tree", int64(len(content)), content)
}

func gitCommitTree() {
	usage := false
	if len(os.Args) < 3 {
		usage = true
	}

	var treeHash, parentHash, message string
	for i := 0; !usage && i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-p":
			if parentHash == "" && i+1 < len(os.Args) {
				parentHash = os.Args[i+1]
			} else {
				usage = true
			}
			i++ // skip
		case "-m":
			if message == "" && i+1 < len(os.Args) {
				message = os.Args[i+1]
			} else {
				usage = true
			}
			i++ // skip
		default:
			treeHash = os.Args[i]
		}
	}

	if usage {
		printUsageAndExit("commit-tree <tree_sha> [-p <parent_sha>] [-m <message>]")
	}

	// make sure tree_sha and parent_sha exists and have the right type
	treeType, _ := getObjTypeAndSize(treeHash)
	if treeType != "tree" {
		fatal("expected '%s' to be a 'tree' object, got: %s", treeHash, treeType)
	}

	parentType, _ := getObjTypeAndSize(treeHash)
	if parentType != "tree" {
		fatal("expected '%s' to be a 'tree' object, got: %s", parentHash, parentType)
	}

	// TODO: Using "git config" itself to get the config for now...
	username, email := getGitConfig("user.name"), getGitConfig("user.email")
	now := time.Now()
	timestamp := now.Unix()
	_, tzOffset := now.Zone()
	tzHours, tzMinutes := tzOffset/3600, (tzOffset/60)%60
	timezone := tzHours*100 + tzMinutes

	content := fmt.Sprintf("tree %s\n", treeHash)
	if parentHash != "" {
		content += fmt.Sprintf("parent %s\n", parentHash)
	}
	content += fmt.Sprintf("author %s <%s> %d %+05d\n", username, email, timestamp, timezone)
	content += fmt.Sprintf("committer %s <%s> %d %+05d\n", username, email, timestamp, timezone)
	content += fmt.Sprintf("\n%s\n", message)

	commitHash := hashObject(true, "commit", int64(len(content)), []byte(content))
	fmt.Printf("%x\n", commitHash)
}

func getGitConfig(key string) string {
	cmd := exec.Command("git", "config", key)
	output, err := cmd.Output()
	if err != nil {
		fatal(err.Error())
	}
	return strings.TrimRight(string(output), "\r\n")
}

func DISABLE_gitClone() {
	if len(os.Args) < 4 {
		printUsageAndExit("clone <repo> <dir>")
	}

	repoUrl := os.Args[2]
	directory := os.Args[3]
	_ = directory

	respGet, err := http.Get(repoUrl + "/info/refs?service=git-upload-pack")
	if err != nil {
		fatal(err.Error())
	}
	defer respGet.Body.Close()

	if respGet.StatusCode != 200 {
		fatal("could not fetch %q - status code: %d", repoUrl, respGet.StatusCode)
	}

	contentType := respGet.Header.Get("Content-Type")
	if contentType != "application/x-git-upload-pack-advertisement" {
		fatal("unexpected content type: %q", contentType)
	}

	refs := map[string]string{}

	// start parsing the pack response
	sizeBuffer := make([]byte, 4)
	for {
		_, err := respGet.Body.Read(sizeBuffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fatal(err.Error())
		}

		size, err := strconv.ParseUint(string(sizeBuffer), 16, 32)
		if err != nil {
			fatal(err.Error())
		}

		if size > 4 {
			dataBuffer := make([]byte, size-4)
			_, err = respGet.Body.Read(dataBuffer)
			if err != nil {
				fatal(err.Error())
			}

			fmt.Printf("size=%d data=%q\n", size, string(dataBuffer))

			// strip newline
			data := string(dataBuffer)
			if data[len(data)-1] == '\n' {
				data = data[:len(data)-1]
			}

			if data[0] == '#' {
				if data != "# service=git-upload-pack" {
					fatal("unexpected header: %q", data)
				}
			} else {
				hash := data[:40]
				// ignore whitespace
				parts := strings.Split(data[41:], "\000")
				ref := parts[0]
				if len(parts) > 1 {
					capabilities := strings.Split(parts[1], " ")
					for _, capability := range capabilities {
						fmt.Printf("capability: %s\n", capability)
					}
				}
				fmt.Printf("hash=%s ref=%s\n", hash, ref)
				refs[ref] = hash
			}
		} else {
			fmt.Printf("size=%d (terminator)\n", size)
		}
	}

	for ref, hash := range refs {
		fmt.Println(ref, hash)
	}

	if _, ok := refs["HEAD"]; !ok {
		fatal("no HEAD reference found")
	}

	fmt.Println("====")

	postHeader := "application/x-git-upload-pack-request"
	postBody := fmt.Sprintf("0032want %s\n00000009done\n", refs["HEAD"])
	respPost, err := http.Post(repoUrl+"/git-upload-pack", postHeader, strings.NewReader(postBody))
	if err != nil {
		fatal(err.Error())
	}
	defer respPost.Body.Close()

	if respGet.StatusCode != 200 {
		fatal("could not fetch %q - status code: %d", repoUrl, respGet.StatusCode)
	}

	// contentType = respGet.Header.Get("Content-Type")
	// if contentType != "application/x-git-upload-pack-result" {
	// 	fatal("unexpected content type: %q", contentType)
	// }

	// TODO: decode the pack file

	nakExpected := []byte("0008NAK\n")
	nakHeader := make([]byte, 8)
	_, err = respPost.Body.Read(nakHeader)
	if err != nil {
		fatal(err.Error())
	}
	if slices.Compare(nakExpected, nakHeader) != 0 {
		fatal("unexpected header on response. got: %q - want: %q\n", nakHeader, nakExpected)
	}

	file, err := os.Create("example.pack")
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	io.Copy(file, respPost.Body)
	fmt.Println("result saved to file example.pack")
}

const OBJ_COMMIT = 1
const OBJ_TREE = 2
const OBJ_BLOB = 3
const OBJ_TAG = 4
const OBJ_OFS_DELTA = 6
const OBJ_REF_DELTA = 7

func gitClone() {
	fmt.Println("skipped fetch, loading example.pack")
	file, err := os.Open("example.pack")
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// begin reading the pack file now
	packHeader := make([]byte, 12)
	_, err = reader.Read(packHeader)
	if err != nil {
		fatal(err.Error())
	}
	if slices.Compare(packHeader[:4], []byte("PACK")) != 0 {
		fatal("invalid PACK header")
	}
	version := bigEndianBytesToUint(packHeader[4:8])
	objCount := bigEndianBytesToUint(packHeader[8:12])
	fmt.Println(version, objCount)

	offset := uint64(12)
	objRefDelta := make([]byte, 20)
	for index := 0; index < int(objCount); index++ {

		value, err := reader.ReadByte()
		lenghtBytesRead := uint64(1)
		if err != nil {
			fatal(err.Error())
		}

		objType := (value >> 4) & 0b00000111
		informedSize := uint64(value & 0b00001111)
		shift := 4
		for value&0b10000000 != 0 {
			value, err = reader.ReadByte()
			lenghtBytesRead++
			if err != nil {
				fatal(err.Error())
			}
			informedSize = informedSize | uint64(value&0b01111111)<<shift
			if value&0b10000000 == 0 {
				break
			}
			shift += 7
		}

		fmt.Printf("index=%2d\ttype=%d\toffset=%5d\tsize=%5d", index, objType, offset, informedSize)
		if objType == OBJ_OFS_DELTA {
			fatal("OBJ_OFS_DELTA not implemented yet!")
		} else if objType == OBJ_REF_DELTA {
			reader.Read(objRefDelta)
		}
		// NOTE: no idea why reported size is too small in some cases...
		if informedSize < 1024 {
			informedSize = 1024
		}
		compressedBuffer, err := reader.Peek(int(informedSize))
		if err != nil && err != io.EOF {
			fatal(err.Error())
		}
		bytesBuffer := bytes.NewReader(compressedBuffer)
		zreader, err := zlib.NewReader(bytesBuffer)
		if err != nil {
			fatal(err.Error())
		}
		uncompressedBuffer := make([]byte, informedSize)
		actualSize, err := zreader.Read(uncompressedBuffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			fatal(err.Error())
		}

		compressedBytesRead := uint64(bytesBuffer.Size() - int64(bytesBuffer.Len()))
		fmt.Printf("\tactual_size=%5d\tcompressed_size=%5d", uint64(actualSize), +compressedBytesRead)

		if objType == OBJ_REF_DELTA {
			fmt.Printf("\tobjRefDelta=%x", objRefDelta)
		}

		fmt.Printf("\tcontent=%q\n", uncompressedBuffer[:actualSize])

		reader.Discard(int(compressedBytesRead))
		offset += uint64(lenghtBytesRead + compressedBytesRead)
	}
}

func bigEndianBytesToUint(b []byte) uint {
	return uint(b[0])<<24 | uint(b[1])<<16 | uint(b[2])<<8 | uint(b[3])
}
