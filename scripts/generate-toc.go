// TOC Generator - generates Table of Contents for markdown files
// Replaces content between <!-- START doctoc generated TOC --> and <!-- END doctoc generated TOC --> markers
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode"
)

const startMarker = "<!-- START doctoc generated TOC please keep comment here to allow auto update -->"
const endMarker = "<!-- END doctoc generated TOC please keep comment here to allow auto update -->"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run generate-toc.go <file1.md> [file2.md] ...")
		os.Exit(1)
	}

	for _, filepath := range os.Args[1:] {
		if err := processFile(filepath); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", filepath, err)
			os.Exit(1)
		}
	}
}

func processFile(filepath string) error {
	// G304: script operates on known local files
	// #nosec
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	startIdx := bytes.Index(content, []byte(startMarker))
	if startIdx == -1 {
		return nil // No marker found, skip
	}

	endIdx := bytes.Index(content, []byte(endMarker))
	if endIdx == -1 {
		return fmt.Errorf("found start marker but no end marker")
	}

	// Extract headers from content AFTER the end marker (the actual document content)
	// This is the real content that should be in the TOC
	afterContent := content[endIdx+len(endMarker):]
	headers := extractHeaders(string(afterContent))

	// Generate new TOC
	toc := generateTOC(headers)
	remaining := bytes.TrimLeft(content[endIdx+len(endMarker):], "\r\n")

	// Build new content
	var newContent bytes.Buffer
	newContent.Write(content[:startIdx])
	newContent.WriteString(startMarker)
	newContent.WriteString("\n")
	newContent.WriteString(toc)
	newContent.WriteString(endMarker)
	newContent.WriteString("\n\n")
	newContent.Write(remaining)

	// G304: script operates on known local paths
	// #nosec
	return os.WriteFile(filepath, newContent.Bytes(), 0600)
}

func extractHeaders(content string) []string {
	var headers []string
	var fence string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if fence == "" {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fence = trimmed[:3]
				continue
			}
		} else {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		// Match H1, H2, H3 headers
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			headers = append(headers, line)
		}
	}
	return headers
}

func generateTOC(headers []string) string {
	if len(headers) == 0 {
		return ""
	}

	var buf bytes.Buffer
	buf.WriteString("**Table of Contents**  *generated automatically*\n\n")
	buf.WriteString("<!---toc start-->\n\n")

	for _, header := range headers {
		level := 0
		if strings.HasPrefix(header, "### ") {
			level = 2
		} else if strings.HasPrefix(header, "## ") {
			level = 1
		}

		// Extract header text
		text := strings.TrimPrefix(header, "### ")
		text = strings.TrimPrefix(text, "## ")
		text = strings.TrimPrefix(text, "# ")

		// Generate anchor link
		anchor := generateAnchor(text)

		// Indent based on level
		indent := strings.Repeat("  ", level)
		fmt.Fprintf(&buf, "%s* [%s](#%s)\n", indent, text, anchor)
	}

	buf.WriteString("\n<!---toc end-->\n")
	return buf.String()
}

func generateAnchor(text string) string {
	// Convert to lowercase
	anchor := strings.ToLower(text)
	// Replace spaces with hyphens
	anchor = strings.ReplaceAll(anchor, " ", "-")
	// Remove non-alphanumeric characters except hyphens
	var result strings.Builder
	for _, r := range anchor {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}
