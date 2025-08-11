package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-shiori/go-readability"
	html2markdown "github.com/JohannesKaufmann/html-to-markdown"
)

// sanitizeFileName creates a safe file name from the article title.
// It replaces characters that are illegal on most file systems with an underscore
// and collapses consecutive spaces/underscores.
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	// Characters not allowed in Windows filenames and also problematic on Unix.
	illegal := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	name = illegal.ReplaceAllString(name, "_")
	// Collapse multiple spaces or underscores into a single underscore.
	collapse := regexp.MustCompile(`[\s_]+`)
	name = collapse.ReplaceAllString(name, "_")
	return name
}

// fetchURL downloads the content of the given URL and returns it as a byte slice.
func fetchURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func main() {
	// Command‑line flags
	articleURL := flag.String("url", "", "Full URL of the Habr article to download (required)")
	outputDir := flag.String("out", ".", "Directory where the markdown file will be saved")
	flag.Parse()

	if *articleURL == "" {
		fmt.Fprintln(os.Stderr, "error: -url flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// 1. Download the page
	rawHTML, err := fetchURL(*articleURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch URL: %v\n", err)
		os.Exit(1)
	}

	// 2. Parse the base URL for readability
	parsedURL, err := url.Parse(*articleURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid URL provided: %v\n", err)
		os.Exit(1)
	}

	// 3. Extract the main article using go‑readability
	article, err := readability.FromReader(strings.NewReader(string(rawHTML)), parsedURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse article: %v\n", err)
		os.Exit(1)
	}

	// 4. Convert the article HTML to Markdown
	converter := html2markdown.NewConverter("", true, nil)
	markdownBody, err := converter.ConvertString(article.Content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to convert HTML to markdown: %v\n", err)
		os.Exit(1)
	}

	// 5. Build a safe filename from the article title
	fileName := sanitizeFileName(article.Title) + ".md"
	fullPath := filepath.Join(*outputDir, fileName)

	// 6. Write the markdown to disk
	if err := os.WriteFile(fullPath, []byte(markdownBody), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Article saved to %s\n", fullPath)
}