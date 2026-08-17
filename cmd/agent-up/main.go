package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/billy4479/agent-up/internal/client"
)

type options struct {
	path string
	name string
	mime string
}

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout, http.DefaultClient); err != nil {
		fmt.Fprintln(os.Stderr, "agent-up:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer, httpClient *http.Client) error {
	options, err := parseArgs(args)
	if err != nil {
		return err
	}
	baseURL := getenv("AGENTUP_URL")
	if baseURL == "" {
		return fmt.Errorf("AGENTUP_URL is not set; set it to the agent-up server URL")
	}
	uploader, err := client.New(baseURL, httpClient)
	if err != nil {
		return err
	}

	var result string
	if options.path == "-" {
		if options.name == "" {
			return fmt.Errorf("stdin upload has no filename; provide one with --name FILENAME")
		}
		result, err = uploader.UploadFile(options.name, options.mime, stdin)
		if err != nil {
			return fmt.Errorf("failed to upload stdin as %q: %w", options.name, err)
		}
	} else {
		info, statErr := os.Lstat(options.path)
		if statErr != nil {
			return fmt.Errorf("cannot access upload path %q: %w", options.path, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("upload path %q is a symlink; symlinks are not supported", options.path)
		}
		if info.IsDir() {
			if options.name != "" {
				return fmt.Errorf("--name cannot be used with directory upload %q", options.path)
			}
			if options.mime != "" {
				return fmt.Errorf("--mime cannot be used with directory upload %q", options.path)
			}
			result, err = uploader.UploadDirectory(options.path)
			if err != nil {
				return fmt.Errorf("failed to upload directory %q: %w", options.path, err)
			}
		} else if info.Mode().IsRegular() {
			if options.name != "" {
				return fmt.Errorf("--name cannot be used with file upload %q; it is only valid for stdin", options.path)
			}
			file, openErr := os.Open(options.path)
			if openErr != nil {
				return fmt.Errorf("cannot open upload file %q: %w", options.path, openErr)
			}
			result, err = uploader.UploadFile(filepath.Base(options.path), options.mime, file)
			closeErr := file.Close()
			if err != nil {
				return fmt.Errorf("failed to upload file %q: %w", options.path, err)
			}
			if closeErr != nil {
				return fmt.Errorf("failed to close upload file %q: %w", options.path, closeErr)
			}
		} else {
			return fmt.Errorf("upload path %q is not a regular file or directory", options.path)
		}
	}
	if _, err = fmt.Fprintln(stdout, result); err != nil {
		return fmt.Errorf("failed to write uploaded URL to stdout: %w", err)
	}
	return nil
}

func parseArgs(args []string) (options, error) {
	var result options
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--mime" || argument == "--name":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s has no value; provide %s VALUE", argument, argument)
			}
			i++
			if argument == "--mime" {
				result.mime = args[i]
			} else {
				result.name = args[i]
			}
		case strings.HasPrefix(argument, "--mime="):
			result.mime = strings.TrimPrefix(argument, "--mime=")
		case strings.HasPrefix(argument, "--name="):
			result.name = strings.TrimPrefix(argument, "--name=")
		case strings.HasPrefix(argument, "-") && argument != "-":
			return options{}, fmt.Errorf("unknown option %q; supported options are --mime and --name", argument)
		default:
			if result.path != "" {
				return options{}, fmt.Errorf("multiple upload paths provided: %q and %q; provide exactly one", result.path, argument)
			}
			result.path = argument
		}
	}
	if result.path == "" {
		return options{}, fmt.Errorf("upload path is missing; usage: agent-up [--mime TYPE] [--name FILENAME] PATH")
	}
	if result.mime == "" && containsOption(args, "--mime") {
		return options{}, fmt.Errorf("--mime is empty; provide a MIME type such as text/plain")
	}
	if result.name == "" && containsOption(args, "--name") {
		return options{}, fmt.Errorf("--name is empty; provide a filename such as upload.bin")
	}
	return result, nil
}

func containsOption(args []string, name string) bool {
	for _, argument := range args {
		if argument == name || strings.HasPrefix(argument, name+"=") {
			return true
		}
	}
	return false
}
