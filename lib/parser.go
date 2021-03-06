package lib

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

const (
	registryTypeNpm   = "npm"
	registryTypeImage = "harbor"
)

//RequestMeta ...
type RequestMeta struct {
	RegistryType string
	HasHit       bool
	Metadata     map[string]string
}

type npmPackMeta struct {
	Tags struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
}

//Parser ...
type Parser func(req *http.Request) (RequestMeta, error)

//NpmParser ...
func NpmParser(req *http.Request) (RequestMeta, error) {
	userAgent := req.Header.Get("User-Agent")
	if strings.Contains(userAgent, "npm") {
		npmCmd := req.Header.Get("Referer")
		if len(npmCmd) > 0 {
			//Hit only when the command existing
			meta := RequestMeta{
				RegistryType: registryTypeNpm,
				HasHit:       true,
				Metadata:     make(map[string]string),
			}
			commands := strings.Split(npmCmd, " ")
			command := strings.TrimSpace(commands[0])
			meta.Metadata["command"] = command
			meta.Metadata["path"] = req.URL.String()
			meta.Metadata["extra"] = strings.TrimSpace(strings.TrimPrefix(npmCmd, command))
			meta.Metadata["session"] = req.Header.Get("Npm-Session")

			//Read more info
			if command == "publish" {
				buf, err := ioutil.ReadAll(req.Body)
				if err != nil {
					return RequestMeta{}, err
				}
				npmMetaJSON := &npmPackMeta{}
				if err := json.Unmarshal(buf, npmMetaJSON); err != nil {
					return RequestMeta{}, err
				}

				meta.Metadata["extra"] = npmMetaJSON.Tags.Latest

				body := ioutil.NopCloser(bytes.NewBuffer(buf))
				req.Body = body
				req.ContentLength = int64(len(buf))
				req.Header.Set("Content-Length", strconv.Itoa(len(buf)))
			}

			return meta, nil
		}
	}

	return RequestMeta{}, nil
}

//HarborParser ...
//Treat as deafault now
func HarborParser(req *http.Request) (RequestMeta, error) {
	return RequestMeta{
		RegistryType: registryTypeImage,
		HasHit:       true, //default handler
	}, nil
}

//ParserChain ...
type ParserChain struct {
	head *parserWrapper
	tail *parserWrapper
}

//ParserWrapper ...
type parserWrapper struct {
	parser Parser
	next   *parserWrapper
}

//Parse ...
func (pc *ParserChain) Parse(req *http.Request) (RequestMeta, error) {
	if pc.head == nil {
		return RequestMeta{}, errors.New("no parsers")
	}

	var errs []string
	p := pc.head
	for p != nil && p.parser != nil {
		if meta, err := p.parser(req); err != nil {
			errs = append(errs, err.Error())
		} else {
			if meta.HasHit {
				return meta, nil
			}
		}

		//next
		p = p.next
	}

	//No hit
	return RequestMeta{}, fmt.Errorf("%s:%s", "no hit", strings.Join(errs, ";"))
}

//Init ...
func (pc *ParserChain) Init() error {
	pc.head = nil
	pc.tail = nil

	if err := pc.Register(NpmParser); err != nil {
		return err
	}

	return pc.Register(HarborParser)
}

//Register ...
func (pc *ParserChain) Register(parser Parser) error {
	if parser == nil {
		return errors.New("nil parser")
	}

	if pc.head == nil {
		pc.head = &parserWrapper{parser, nil}
		pc.tail = pc.head

		return nil
	}

	pc.tail.next = &parserWrapper{parser, nil}
	pc.tail = pc.tail.next

	return nil
}
