package client

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
)

const (
	mmapHeaderSize  = 8
	mmapTokenBytes  = 128
	mmapPayloadSize = 128 * 1024 * 1024
)

type mmapChunk struct {
	offset int
	length int
}

type mmapArea struct {
	data       []byte
	file       *os.File
	filename   string
	token      *big.Int
	tokenIndex int
	tokenBytes int
	mapped     bool
	enabled    bool
}

// EnableMmap prepares a shared memory area for receiving screen updates. It
// must be called before Run. Negotiation remains optional: if the server does
// not accept the area, ordinary encoded draw packets continue to work.
func (c *Client) EnableMmap() error {
	if c.mmap != nil {
		return errors.New("mmap is already enabled")
	}
	area, err := newMmapArea()
	if err != nil {
		return err
	}
	c.mmap = area
	return nil
}

func (c *Client) negotiateMmap(caps protocol.Dict) error {
	if c.mmap == nil {
		return nil
	}
	accepted, err := c.mmap.enableFromCaps(caps.Dict("mmap"))
	if err != nil {
		return err
	}
	if !accepted {
		if err := c.mmap.close(); err != nil {
			log.Printf("cleaning declined mmap area: %v", err)
		}
		c.mmap = nil
		return nil
	}
	if err := c.mmap.removeBackingFile(); err != nil {
		log.Printf("removing negotiated mmap file: %v", err)
	}
	log.Printf("enabled mmap screen updates using a %d MiB shared area", len(c.mmap.data)/1024/1024)
	return nil
}

func (c *Client) closeMmap() {
	if c.mmap == nil {
		return
	}
	if err := c.mmap.close(); err != nil {
		log.Printf("closing mmap area: %v", err)
	}
	c.mmap = nil
}

func newMmapArea() (*mmapArea, error) {
	pageSize := os.Getpagesize()
	size := mmapPayloadSize + mmapHeaderSize
	size = (size + pageSize - 1) / pageSize * pageSize

	file, err := os.CreateTemp("", "go-xpra-*.mmap")
	if err != nil {
		return nil, fmt.Errorf("creating mmap file: %w", err)
	}
	filename := file.Name()
	cleanup := func() {
		file.Close()
		os.Remove(filename)
	}
	if err := file.Truncate(int64(size)); err != nil {
		cleanup()
		return nil, fmt.Errorf("sizing mmap file: %w", err)
	}
	data, err := mapMmapFile(file, size)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("mapping mmap file: %w", err)
	}

	area := &mmapArea{
		data:       data,
		file:       file,
		filename:   filename,
		tokenBytes: mmapTokenBytes,
		mapped:     true,
	}
	if err := area.generateToken(); err != nil {
		area.close()
		return nil, err
	}
	return area, nil
}

func (a *mmapArea) generateToken() error {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generating mmap token: %w", err)
	}
	a.token = new(big.Int).SetBytes(tokenBytes)
	if a.token.Sign() == 0 {
		a.token.SetInt64(1)
	}

	available := len(a.data) - mmapHeaderSize - a.tokenBytes + 1
	if available <= 0 {
		return errors.New("mmap area is too small for its token")
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(available)))
	if err != nil {
		return fmt.Errorf("choosing mmap token position: %w", err)
	}
	a.tokenIndex = mmapHeaderSize + int(index.Int64())
	return writeMmapToken(a.data, a.token, a.tokenIndex, a.tokenBytes)
}

func (a *mmapArea) helloCaps() rencodeplus.Dict {
	area := rencodeplus.Dict{
		{Key: "file", Value: a.filename},
		{Key: "size", Value: len(a.data)},
		{Key: "token", Value: a.token},
		{Key: "token_index", Value: a.tokenIndex},
		{Key: "token_bytes", Value: a.tokenBytes},
	}
	mmapCaps := rencodeplus.Dict{{Key: "read", Value: area}}
	if protocol.BackwardsCompatible {
		for _, entry := range area {
			mmapCaps.Set(entry.Key, entry.Value)
		}
	}
	return mmapCaps
}

func (a *mmapArea) enableFromCaps(caps protocol.Dict) (bool, error) {
	if caps == nil {
		return false, nil
	}
	areaCaps := caps.Dict("write")
	if areaCaps == nil && caps.Has("token") {
		areaCaps = caps
	}
	if areaCaps == nil || areaCaps.Has("enabled") && !areaCaps.Bool("enabled") {
		return false, nil
	}

	token, ok := mmapInteger(areaCaps["token"])
	if !ok || token.Sign() <= 0 {
		return false, errors.New("server returned an invalid mmap token")
	}
	index := areaCaps.Int("token_index")
	count := areaCaps.Int("token_bytes")
	if count == 0 {
		count = mmapTokenBytes
	}
	if index < 0 || count <= 0 || index > int64(len(a.data)) || count > int64(len(a.data))-index {
		return false, errors.New("server returned mmap token bounds outside the shared area")
	}
	found, err := readMmapToken(a.data, int(index), int(count))
	if err != nil {
		return false, err
	}
	if found.Cmp(token) != 0 {
		return false, errors.New("mmap token verification failed")
	}
	a.enabled = true
	return true, nil
}

func mmapInteger(value any) (*big.Int, bool) {
	switch value := value.(type) {
	case int64:
		return big.NewInt(value), true
	case *big.Int:
		if value == nil {
			return nil, false
		}
		return new(big.Int).Set(value), true
	case big.Int:
		return new(big.Int).Set(&value), true
	default:
		return nil, false
	}
}

func writeMmapToken(data []byte, token *big.Int, index, count int) error {
	if token == nil || token.Sign() < 0 || index < 0 || count <= 0 || index > len(data) || count > len(data)-index {
		return errors.New("invalid mmap token or bounds")
	}
	encoded := token.Bytes()
	if len(encoded) > count {
		return errors.New("mmap token does not fit in the advertised token size")
	}
	clear(data[index : index+count])
	for i := range encoded {
		data[index+i] = encoded[len(encoded)-1-i]
	}
	return nil
}

func readMmapToken(data []byte, index, count int) (*big.Int, error) {
	if index < 0 || count <= 0 || index > len(data) || count > len(data)-index {
		return nil, errors.New("invalid mmap token bounds")
	}
	encoded := make([]byte, count)
	for i := range encoded {
		encoded[count-1-i] = data[index+i]
	}
	return new(big.Int).SetBytes(encoded), nil
}

func (a *mmapArea) readChunks(raw any) ([]byte, func(), error) {
	if !a.enabled {
		return nil, nil, errors.New("mmap screen updates were not negotiated")
	}
	chunks, err := parseMmapChunks(raw, len(a.data))
	if err != nil {
		return nil, nil, err
	}
	end := chunks[len(chunks)-1].offset + chunks[len(chunks)-1].length
	released := false
	release := func() {
		if !released {
			binary.NativeEndian.PutUint32(a.data[:4], uint32(end))
			released = true
		}
	}
	if len(chunks) == 1 {
		chunk := chunks[0]
		return a.data[chunk.offset : chunk.offset+chunk.length], release, nil
	}
	total := chunks[0].length + chunks[1].length
	data := make([]byte, 0, total)
	for _, chunk := range chunks {
		data = append(data, a.data[chunk.offset:chunk.offset+chunk.length]...)
	}
	return data, release, nil
}

func parseMmapChunks(raw any, size int) ([]mmapChunk, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 || len(items) > 2 {
		return nil, errors.New("invalid mmap chunk list")
	}
	chunks := make([]mmapChunk, 0, len(items))
	total := 0
	for _, item := range items {
		pair, ok := item.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("invalid mmap chunk descriptor")
		}
		offset, offsetOK := pair[0].(int64)
		length, lengthOK := pair[1].(int64)
		if !offsetOK || !lengthOK || offset < mmapHeaderSize || length < 0 ||
			offset > int64(size) || length > int64(size)-offset {
			return nil, errors.New("mmap chunk is outside the shared area")
		}
		if length > int64(size-total) {
			return nil, errors.New("mmap chunk sizes overflow")
		}
		chunks = append(chunks, mmapChunk{offset: int(offset), length: int(length)})
		total += int(length)
	}
	if total == 0 {
		return nil, errors.New("mmap chunks contain no pixel data")
	}
	return chunks, nil
}

func (a *mmapArea) removeBackingFile() error {
	var err error
	if a.file != nil {
		err = a.file.Close()
		a.file = nil
	}
	if a.filename != "" {
		if removeErr := os.Remove(a.filename); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		} else {
			a.filename = ""
		}
	}
	return err
}

func (a *mmapArea) close() error {
	var err error
	if a.mapped && a.data != nil {
		err = unmapMmapFile(a.data)
	}
	a.data = nil
	a.mapped = false
	a.enabled = false
	return errors.Join(err, a.removeBackingFile())
}
