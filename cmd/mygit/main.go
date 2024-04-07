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

	file, err := os.Create(directory + ".pack")
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()

	io.Copy(file, respPost.Body)
	fmt.Printf("result saved to file %s.pack\n", directory)
}

const OBJ_COMMIT = 1
const OBJ_TREE = 2
const OBJ_BLOB = 3
const OBJ_TAG = 4
const OBJ_OFS_DELTA = 6
const OBJ_REF_DELTA = 7

type objDelta struct {
	source  []byte
	content []byte
}

func gitClone() {
	directory := os.Args[3]
	fmt.Printf("skipped fetch, loading %s.pack\n", directory)
	file, err := os.Open(directory + ".pack")
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

	// TEMP: just purging and recreating everything while testing
	err = os.RemoveAll("v:/GitHub/feliposz/build-your-own-git-go/test.dir/" + directory)
	if err != nil {
		panic(err)
	}

	// TODO: refactor gitInit to accept parameter to new directory
	// TEMP: make new directory and initialize .git on the new directory
	os.Mkdir(directory, 0755)
	os.Chdir(directory)
	gitInit()

	// TODO: write HEAD and refs
	// TEMP: forcing a specific HEAD for testing (should use the one informed on discovery, as well as branch name!)
	os.MkdirAll(filepath.Join(".git", "refs", "heads"), 0755)
	switch directory {
	case "git-sample-1":
		os.WriteFile(filepath.Join(".git", "refs", "heads", "master"), []byte("47b37f1a82bfe85f6d8df52b6258b75e4343b7fd"), 0644)
	case "git-sample-2":
		os.WriteFile(filepath.Join(".git", "refs", "heads", "master"), []byte("7b8eb72b9dfa14a28ed22d7618b3cdecaa5d5be0"), 0644)
	case "git-sample-3":
		os.WriteFile(filepath.Join(".git", "refs", "heads", "master"), []byte("23f0bc3b5c7c3108e41c448f01a3db31e7064bbb"), 0644)
	}

	// save deltas to apply after unpacking
	savedObjDeltas := []objDelta{}

	// extracting objects from pack file received

	offset := uint64(12)
	for index := 0; index < int(objCount); index++ {

		var objRefDeltaHash []byte

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
			objRefDeltaHash = make([]byte, 20)
			reader.Read(objRefDeltaHash)
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
			fmt.Printf("\tobjRefDelta=%x", objRefDeltaHash)
		}

		fmt.Printf("\tcontent=%q\n", uncompressedBuffer[:actualSize])

		// hash objects and write objects that were read from the pack file

		switch objType {
		case OBJ_BLOB:
			hashObject(true, "blob", int64(actualSize), uncompressedBuffer[:actualSize])
		case OBJ_TREE:
			hashObject(true, "tree", int64(actualSize), uncompressedBuffer[:actualSize])
		case OBJ_COMMIT:
			hashObject(true, "commit", int64(actualSize), uncompressedBuffer[:actualSize])
		case OBJ_TAG:
			hashObject(true, "tag", int64(actualSize), uncompressedBuffer[:actualSize])
		case OBJ_REF_DELTA:
			savedObjDeltas = append(savedObjDeltas, objDelta{objRefDeltaHash, uncompressedBuffer[:actualSize]})
		}

		reader.Discard(int(compressedBytesRead))
		offset += uint64(lenghtBytesRead + compressedBytesRead)
	}

	// applying deltas
	// reference: https://codewords.recurse.com/issues/three/unpacking-git-packfiles#applying-deltas

	for _, savedObjDelta := range savedObjDeltas {
		sourceHash, content := savedObjDelta.source, savedObjDelta.content
		fmt.Printf("parent=%x\tcontent=%v\n", sourceHash, content)

		i := 0
		sourceSize := uint64(content[i]) & 0b01111111
		shift := 7
		for content[i]&0b10000000 != 0 {
			i++
			sourceSize |= uint64(content[i]&0b01111111) << shift
			shift += 7
		}
		fmt.Printf("sourceSize=%d\t", sourceSize)
		i++

		targetSize := uint64(content[i]) & 0b01111111
		shift = 7
		for content[i]&0b10000000 != 0 {
			i++
			targetSize |= uint64(content[i]&0b01111111) << shift
			shift += 7
		}
		fmt.Printf("targetSize=%d\n", targetSize)
		i++

		targetBuffer := make([]byte, targetSize)

		sourceType, readSize, sourceBuffer := readObject(sourceHash)
		// TODO: are other types allowed here?
		if sourceSize != readSize {
			fatal("unexpected source size for delta: got %d - want %d\n", readSize, sourceSize)
		}

		var sourceIndex, targetIndex uint32

		// iterate on instructions (copy/insert)
		for i < len(content) {
			op := content[i] >> 7
			if op == 1 { // copy operation

				// decode which bytes to read that are non-zero from the copy instruction
				lengthBitmap := content[i] >> 4 & 0b0000111
				offsetBitmap := content[i] & 0b00001111
				i++

				offset, length := uint32(0), uint32(0)

				if offsetBitmap&0b0001 != 0 {
					offset |= uint32(content[i])
					i++
				}
				if offsetBitmap&0b0010 != 0 {
					offset |= uint32(content[i]) << 8
					i++
				}
				if offsetBitmap&0b0100 != 0 {
					offset |= uint32(content[i]) << 16
					i++
				}
				if offsetBitmap&0b1000 != 0 {
					offset |= uint32(content[i]) << 24
					i++
				}

				if lengthBitmap&0b0001 != 0 {
					length |= uint32(content[i])
					i++
				}
				if lengthBitmap&0b0010 != 0 {
					length |= uint32(content[i]) << 8
					i++
				}
				if lengthBitmap&0b0100 != 0 {
					length |= uint32(content[i]) << 16
					i++
				}

				sourceIndex = offset
				fmt.Printf("copying %d:%d into %d:%d\n", sourceIndex, sourceIndex+length, targetIndex, targetIndex+length)
				copy(targetBuffer[targetIndex:targetIndex+length], sourceBuffer[sourceIndex:sourceIndex+length])
				targetIndex += length
			} else { // insert operation
				length := uint32(content[i]) // length is on the operation itself 0-127
				i++
				fmt.Printf("inserting %d bytes into %d:%d\n", length, targetIndex, targetIndex+length)
				copy(targetBuffer[targetIndex:targetIndex+length], content[i:i+int(length)])
				targetIndex += length
				i += int(length)
			}
		}

		targetHash := hashObject(true, sourceType, int64(targetSize), targetBuffer)
		fmt.Printf("delta applied source: %x target: %x\n", sourceHash, targetHash)
	}

	// TODO: "checkout" files to workdir
}

func bigEndianBytesToUint(b []byte) uint {
	return uint(b[0])<<24 | uint(b[1])<<16 | uint(b[2])<<8 | uint(b[3])
}

func readObject(hash []byte) (objType string, objSize uint64, content []byte) {

	objPath := filepath.Join(".git", "objects", fmt.Sprintf("%x", hash[:1]), fmt.Sprintf("%x", hash[1:]))
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

	lengthStr, err := reader.ReadString(0)
	lengthStr = lengthStr[:len(lengthStr)-1]
	if err != nil {
		fatal(err.Error())
	}
	objSize, _ = strconv.ParseUint(lengthStr, 10, 64)

	content = make([]byte, objSize)
	_, err = reader.Read(content)
	if err != nil {
		fatal(err.Error())
	}
	return
}
