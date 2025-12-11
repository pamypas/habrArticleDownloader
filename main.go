package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bmaupin/go-epub"
	"github.com/go-shiori/go-readability"
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

// fetchBinary downloads binary content (e.g., images) and returns the data and a guessed file extension.
func fetchBinary(resourceURL string) ([]byte, string, error) {
	resp, err := http.Get(resourceURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	ct := resp.Header.Get("Content-Type")
	ext := ""
	switch {
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		ext = ".jpg"
	case strings.Contains(ct, "png"):
		ext = ".png"
	case strings.Contains(ct, "gif"):
		ext = ".gif"
	case strings.Contains(ct, "webp"):
		ext = ".webp"
	case strings.Contains(ct, "svg"):
		ext = ".svg"
	default:
		ext = ""
	}

	return data, ext, nil
}

func main() {
	// Command‑line flags
	articleURL := flag.String("url", "", "Full URL of the Habr article to download (required)")
	outputDir := flag.String("out", ".", "Directory where the EPUB file will be saved")
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

	// 4. Prepare EPUB
	title := article.Title
	if strings.TrimSpace(title) == "" {
		title = "Habr Article"
	}
	e := epub.NewEpub(title)
	// Author is not always available from readability; set a generic one.
	e.SetAuthor("Habr")

	// 5. Parse article HTML and embed images
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.Content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse article HTML: %v\n", err)
		os.Exit(1)
	}

	imgCounter := 1

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists {
			return
		}
		src = strings.TrimSpace(src)
		if src == "" {
			return
		}

		// Resolve relative URLs against the article URL
		imgURL, err := parsedURL.Parse(src)
		if err != nil {
			return
		}

		data, ext, err := fetchBinary(imgURL.String())
		if err != nil {
			return
		}

		if ext == "" {
			// Try to guess extension from URL path as a fallback
			ext = filepath.Ext(imgURL.Path)
		}
		if ext == "" {
			ext = ".img"
		}

		imgFileName := fmt.Sprintf("image_%03d%s", imgCounter, ext)
		imgCounter++

		// Write image to a stable temp directory that will live until process exit.
		// We do NOT defer os.Remove here, because go-epub reads the file later
		// when e.Write() is called.
		tmpDir := os.TempDir()
		tmpPath := filepath.Join(tmpDir, imgFileName)

		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			return
		}

		// go-epub AddImage expects a filesystem path.
		imgPath, err := e.AddImage(tmpPath, imgFileName)
		if err != nil {
			return
		}

		// Update the img src to point to the EPUB image path
		s.SetAttr("src", imgPath)
	})

	// 6. Serialize modified HTML
	var bodyHTML string
	if bodySel := doc.Find("body"); bodySel.Length() > 0 {
		html, err := bodySel.Html()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to serialize body HTML: %v\n", err)
			os.Exit(1)
		}
		bodyHTML = html
	} else {
		// Fallback: full document HTML
		html, err := doc.Html()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to serialize HTML: %v\n", err)
			os.Exit(1)
		}
		bodyHTML = html
	}

	// Wrap bodyHTML into a minimal HTML document for EPUB section
	var buf bytes.Buffer
	buf.WriteString("<html><head><meta charset=\"utf-8\"></head><body>")
	buf.WriteString(bodyHTML)
	buf.WriteString("</body></html>")

	// 7. Add content as a chapter
	chapterTitle := title
	if strings.TrimSpace(chapterTitle) == "" {
		chapterTitle = "Article"
	}
	_, err = e.AddSection(buf.String(), chapterTitle, "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to add section to EPUB: %v\n", err)
		os.Exit(1)
	}

	// 8. Save EPUB
	fileName := sanitizeFileName(title) + ".epub"
	fullPath := filepath.Join(*outputDir, fileName)

	if err := e.Write(fullPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write EPUB: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("EPUB saved to %s\n", fullPath)
}
