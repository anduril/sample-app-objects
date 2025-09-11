package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/alecthomas/kong"
	api "github.com/anduril/lattice-sdk-go/v2"
	client "github.com/anduril/lattice-sdk-go/v2/client"
	option "github.com/anduril/lattice-sdk-go/v2/option"
)

type cli struct {
	Delete         deleteCmd         `cmd:"" help:"Remove path from object store."`
	Upload         uploadCmd         `cmd:"" help:"Upload a path."`
	ObjectMetadata objectMetadataCmd `cmd:"" help:"Get metadata for a path."`
	Get            getCmd            `cmd:"" help:"Download a path."`
	List           listCmd           `cmd:"" help:"List all paths in the lattice mesh."`
}

type deleteCmd struct {
	connectionOpts
	Path string `short:"p" name:"path" help:"Path to remove." type:"string" required:""`
}

func (d *deleteCmd) Run(kongCtx *kong.Context) error {
	objectStoreClient := objectStoreClient(d.BaseURL, d.LatticeVMToken, d.LatticeEnvToken, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := objectStoreClient.Objects.DeleteObject(ctx, d.Path); err != nil {
		return fmt.Errorf("unable to delete path %q: %w", d.Path, err)
	}
	fmt.Printf("deleted path %q\n", d.Path)
	return nil
}

type uploadCmd struct {
	connectionOpts
	InputPath       string `short:"i" required:"" name:"input-path"        help:"Path to upload."             type:"path"`
	ObjectStorePath string `short:"p" required:"" name:"object-store-path" help:"Target path in object store" type:"string"`
	TimeToLive      string `short:"t"             name:"time-to-live"      help:"ttl duration string"`
}

func (u *uploadCmd) Run(kongCtx *kong.Context) error {
	var ttl time.Duration
	if len(u.TimeToLive) > 0 {
		parsedDuration, err := time.ParseDuration(u.TimeToLive)
		if err != nil {
			return fmt.Errorf("unable to parse duration %q: %w", u.TimeToLive, err)
		}
		ttl = parsedDuration
	}

	ttlHeader := http.Header{}
	ttlHeader.Add("Time-To-Live", strconv.FormatInt(int64(ttl), 10))

	objectStoreClient := objectStoreClient(
		u.BaseURL,
		u.LatticeVMToken,
		u.LatticeEnvToken,
		ttlHeader,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fileReader, err := os.Open(u.InputPath)
	if err != nil {
		return fmt.Errorf("unable to open file %q: %w", u.InputPath, err)
	}
	defer fileReader.Close()

	fileBytes, err := io.ReadAll(fileReader)
	if err != nil {
		return fmt.Errorf("unable to read bytes for file %q: %w", u.InputPath, err)
	}

	pathMetadata, err := objectStoreClient.Objects.UploadObject(ctx, u.ObjectStorePath, bytes.NewReader(fileBytes))
	if err != nil {
		return fmt.Errorf("unable to upload file to object store: %w", err)
	}
	jsonStr, err := json.Marshal(pathMetadata)
	if err != nil {
		return fmt.Errorf("unable to parse response %v as JSON: %w", pathMetadata, err)
	}
	fmt.Printf("%s\n", jsonStr)
	return nil
}

type objectMetadataCmd struct {
	connectionOpts
	Path string `short:"p" required:"" name:"path" help:"Target path for metadata." type:"string"`
}

func (o *objectMetadataCmd) Run(kongCtx *kong.Context) error {
	objectStoreClient := objectStoreClient(o.BaseURL, o.LatticeVMToken, o.LatticeEnvToken, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	header, err := objectStoreClient.Objects.WithRawResponse.GetObjectMetadata(ctx, o.Path)
	if err != nil {
		return fmt.Errorf("unable to get object metadata for path %q, err=%w", o.Path, err)
	}
	jsonStr, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("unable to parse response %v as JSON: %w", header, err)
	}
	fmt.Printf("%s\n", jsonStr)
	return nil
}

type getCmd struct {
	connectionOpts
	ObjectStorePath string `short:"p" required:"" name:"object-store-path"   help:"Path to remove."                                               type:"string"`
	OutputPath      string `short:"o" required:"" name:"output-path"         help:"Output path to save file."                                     type:"path"`
	ReplaceFile     bool   `short:"r"             name:"replace-output-path" help:"If set, replaces the output path with the downloaded contents"`
}

func (o *getCmd) Run(kongCtx *kong.Context) error {
	objectStoreClient := objectStoreClient(o.BaseURL, o.LatticeVMToken, o.LatticeEnvToken, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	request := &api.GetObjectRequest{}
	objectReader, err := objectStoreClient.Objects.GetObject(ctx, o.ObjectStorePath, request)
	if err != nil {
		return fmt.Errorf("unable to get file %q from object store: %w", o.ObjectStorePath, err)
	}

	outputWriter, err := os.Create(o.OutputPath)
	if err != nil {
		return fmt.Errorf("unable to create writer for %q: %w", o.OutputPath, err)
	}
	defer outputWriter.Close()

	bytesCopied, err := io.Copy(outputWriter, objectReader)
	if err != nil {
		return fmt.Errorf("unable to write contents to file %q (copied %d bytes): %w", o.OutputPath, bytesCopied, err)
	}
	fmt.Printf("wrote %d bytes to file %q\n", bytesCopied, o.OutputPath)
	return nil
}

type listCmd struct {
	connectionOpts
	Prefix string `arg:"" optional:"" name:"prefix" help:"Prefix to list." type:"string"`
}

func (l *listCmd) Run(kongCtx *kong.Context) error {
	objectStoreClient := objectStoreClient(l.BaseURL, l.LatticeVMToken, l.LatticeEnvToken, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &api.ListObjectsRequest{}
	if len(l.Prefix) > 0 {
		req.Prefix = &l.Prefix
	}
	page, err := objectStoreClient.Objects.ListObjects(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to list objects page: %w", err)
	}
	pageIterator := page.Iterator()
	for pageIterator.Next(ctx) {
		pathMetadata := pageIterator.Current()
		pathMetadataStr, err := pathMetadataStr(pathMetadata)
		if err != nil {
			return err
		}
		fmt.Println(pathMetadataStr)
	}
	if err := pageIterator.Err(); err != nil {
		return fmt.Errorf("error listing path metadatas: %w", err)
	}
	return nil
}

func pathMetadataStr(pathMetadata *api.PathMetadata) (string, error) {
	enc, err := json.Marshal(pathMetadata)
	if err != nil {
		return "", fmt.Errorf("unable to parse response %v as JSON: %w", pathMetadata, err)
	}
	return string(enc), nil
}

func objectStoreClient(
	url string,
	vmToken string,
	latticeToken string,
	additionalHeaders http.Header,
) *client.Client {
	header := http.Header{}
	header.Add("authorization", fmt.Sprintf("Bearer %s", vmToken))
	header.Add("anduril-sandbox-authorization", fmt.Sprintf("Bearer %s", latticeToken))

	for headerKey, headerValues := range additionalHeaders {
		for _, headerValue := range headerValues {
			header.Add(headerKey, headerValue)
		}
	}

	return client.NewClient(
		option.WithBaseURL(url),
		option.WithHTTPHeader(header),
	)
}

type connectionOpts struct {
	BaseURL         string `short:"b" name:"base-url"          required:""`
	LatticeVMToken  string `short:"v" name:"lattice-vm-token"  required:""`
	LatticeEnvToken string `short:"e" name:"lattice-env-token" required:""`
}
