package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/temoto/robotstxt"
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

		p.SaveResources()

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
func (p *Page) SaveResources() {
	direxp := regexp.MustCompile("/[[:alpha:]]+/")
	for _, res := range p.Resources {
		if _, err := os.Stat(res); !os.IsNotExist(err) {
			// file exists so don't save it
			continue
		}
		// get path and save path
		// trim /html/
		getpath := strings.TrimPrefix(res, direxp.FindString(res))

		//handle wordpress path
		resurl, _ := url.Parse(getpath)
		direxp := regexp.MustCompile("/$")
		if direxp.MatchString(resurl.Path) {
			resurl.Path = resurl.Path[:len(resurl.Path)-1]
		}
		// save to path ex.:
		// from /img/http://www.indiajoze.com/images/haji_firuz.jpg
		// to /img/www.indiajoze.com/images/haji_firuz.jpg
		splitpath := strings.Split(res, "/")
		savep := SavePath + "/" + splitpath[1] + "/" + splitpath[4] + resurl.Path

		//check robots txt
		resp, err := http.Get(resurl.Scheme + "://" + resurl.Host + "/robots.txt")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad url [%s]\n", err)
			continue
		}
		robots, _ := robotstxt.FromResponse(resp)
		if !robots.TestAgent(getpath, Agent) {
			saveRobots(savep)
			continue
		}

		p.Link = getpath
		err = p.FetchBody()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with Get Request during Save\n")
			// keep going
			continue
		}

		b, err := ioutil.ReadAll(p.Response.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading body. [%s]\n", err)
			continue
		}

		f, err := createFile(savep)
		if err != nil {
			continue
		}
		//
		//		err = os.MkdirAll(filepath.Dir(savep), os.ModePerm)
		//		if err != nil {
		//			fmt.Fprintf(os.Stderr, "Error creating path to save. [%s]\n", err)
		//			continue
		//		}
		//
		//		fmt.Println("Saving File ", savep)
		//		f, err := os.Create(savep)
		//		if err != nil {
		//			fmt.Fprintf(os.Stderr, "Error creating file. [%s]\n", err)
		//			continue
		//		}

		n, err := f.Write(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file. [%s]\n", err)
		}
		fmt.Printf("Saving %d bytes to %s\n", n, savep)

		f.Close()
		p.Response.Body.Close()

	}
}

func saveRobots(path string) {
	f, err := createFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file")
		return
	}
	b := []byte("<h1>This file has been protected by robots.txt<h1>")
	_, err = f.Write(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing robot file. [%s]\n", err)
	}
	f.Close()

	fmt.Println("File protected by robots.txt ", path)
}

func createFile(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating path to save. [%s]\n", err)
		return nil, err
	}

	fmt.Println("Saving File ", path)
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file. [%s]\n", err)
		return nil, err
	}
	return f, nil
}
