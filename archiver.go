package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	SavePath = "Archive"
	Agent    = "ArchiveBot1.0"
)

type Page struct {
	Link string

	// full url from original scrape
	Url *url.URL

	// base file path to save to
	Path string

	//pointer to body
	Response *http.Response

	// array of resource files to download
	Resources []string
}

// snsArchiver takes a file with a list of url's and
// scans them for resources then saves them in a format
// readable by the archive player
func main() {
	flag.Parse()
	file := flag.Arg(0)
	if file == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s <path to link file>\n", os.Args[0])
		os.Exit(1)
	}

	linkFile, err := os.Open(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open %s for reading\n", file)
		os.Exit(1)
	}
	linkReader := bufio.NewReader(linkFile)
	scanner := bufio.NewScanner(linkReader)
	scanner.Split(bufio.ScanLines)

	// iterate through links and save resource files
	p := Page{}
	for scanner.Scan() {
		link := scanner.Text()
		// handle blank lines in txt file
		if link == "" {
			continue
		}
		linkUrl, err := url.Parse(link)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad url from file.[%s]\n", err)
			continue
		}
		p.Url = linkUrl
		p.Link = link

		fmt.Println(linkUrl.Scheme)
		if linkUrl.Scheme == "ftp" {
			// do something with ftp
			os.Exit(0)
		}

		err = p.FetchBody()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching resources\n[%s]\n", err)
		}

		err = p.UpdateHtml()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating html\n[%s]\n", err)
		}

		//		err = p.saveResources()
		//		if err != nil {
		//			fmt.Fprintf(os.stderr, "Error saving resources\n[%s]\n", err)
		//		}

	}
	for _, res := range p.Resources {
		fmt.Println("Resource ", res)
	}
	fmt.Println("completed successfully!")
}

func (p *Page) FetchBody() error {
	// get resp.body
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	timeout := time.Duration(30 * time.Second)
	client := http.Client{Transport: transport, Timeout: timeout}
	req, err := http.NewRequest("GET", p.Link, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", Agent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	p.Response = resp

	return nil

}

func (p *Page) UpdateHtml() error {
	// open file for writing
	// html resource in html dir
	if p.Url.Path == "/" || p.Url.Path == "" {
		p.Url.Path = "/index.html"
	}

	//handle wordpress path
	direxp := regexp.MustCompile("/$")
	if direxp.MatchString(p.Url.Path) {
		p.Url.Path = p.Url.Path[:len(p.Url.Path)-1]
	}

	path := SavePath + "/html/" + p.Url.Host + p.Url.Path

	err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file to save html\n")
		return err
	}
	defer file.Close()

	//update html
	doc, err := html.Parse(p.Response.Body)
	if err != nil {
		return err
	}

	p.ScanHtml(doc)
	err = html.Render(file, doc)
	if err != nil {
		return err
	}
	return nil
}

// Parse html and replace links with local links relative to archive file
// appends resources to be saved later
func (p *Page) ScanHtml(n *html.Node) {
	if n.Type == html.ElementNode {
		switch strings.ToLower(n.Data) {
		case "a":
			p.ReplaceLink("html", "href", n)
			// don't append link
		case "img":
			p.ReplaceLink("img", "src", n)
		case "link":
			p.ReplaceLink("resource", "href", n)
		case "xlink":
			p.ReplaceLink("resource", "href", n)
		case "script":
			p.ReplaceLink("script", "src", n)
		case "use":
			p.ReplaceLink("resource", "href", n)
		case "iframe":
			p.ReplaceLink("resource", "hfef", n)
		case "video":
			p.ReplaceLink("resource", "src", n)
		case "audio":
			p.ReplaceLink("resource", "src", n)
		case "object":
			p.ReplaceLink("resource", "data", n)
		case "embed":
			p.ReplaceLink("resource", "src", n)
		case "span":
			p.ReplaceLink("resource", "data-src", n)

		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		p.ScanHtml(c)
	}
}

func (p *Page) ReplaceLink(elem, key string, n *html.Node) {
	link := ""
	//handle "data-*"
	re := regexp.MustCompile("htm")
	for i := 0; i < len(n.Attr); i++ {
		if strings.ToLower(n.Attr[i].Key) == key {
			u, err := url.Parse(n.Attr[i].Val)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing url: [%s]\n", err)
			}
			// skip mail ref
			if elem == "html" && u.Scheme == "mailto" {
				break
			}

			link = p.Url.ResolveReference(u).String()
			link = "/" + elem + "/" + link
			n.Attr[i].Val = link

			// dont apppend html files to resource list
			// match htm shtml html
			if elem == "html" && re.MatchString(filepath.Ext(link)) {
				break
			}

			p.Resources = append(p.Resources, link)
		}
	}
}

// TODO iterate through resources and save them to right dir
// func SaveResources
// iterate through resources
// get path to load use fetch with page link as url returns io.Reader
// get path to save should be the same.  handle trailing slash
// check if file exists already dont over write
