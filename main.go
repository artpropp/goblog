package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/russross/blackfriday"
)

type Page struct {
	Title      string
	LastChange time.Time
	Content    template.HTML
	Comments   []Comment
}

type Pages []Page

type Comment struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

var (
	flagSrcFolder   = flag.String("src", "./pages/", "blog folder")
	flagTmplFolder  = flag.String("tmpl", "./templates/", "template folder")
	flagFilesFolder = flag.String("files", "./files/", "path for the file server")
	flagPort        = flag.String("port", "8001", "port of the webserver")
)

func loadPage(fpath string) (Page, error) {
	var p Page
	fi, err := os.Stat(fpath)
	if err != nil {
		return p, fmt.Errorf("loadPage: %w", err)
	}
	p.Title = fi.Name()
	p.LastChange = fi.ModTime()
	p.Comments, err = loadComments(p.Title)
	if err != nil {
		return p, fmt.Errorf("loadPage.loadComments: %w", err)
	}
	b, err := ioutil.ReadFile(fpath)
	if err != nil {
		return p, fmt.Errorf("loadPage.ReadFile: %w", err)
	}
	p.Content = template.HTML(blackfriday.MarkdownCommon(b))
	return p, nil
}

func loadPages(src string) (Pages, error) {
	var ps Pages
	fs, err := ioutil.ReadDir(src)
	if err != nil {
		return ps, fmt.Errorf("loadPages.ReadDir: %w", err)
	}
	for _, f := range fs {
		if f.IsDir() {
			continue
		}
		fpath := filepath.Join(src, f.Name())
		p, err := loadPage(fpath)
		if err != nil {
			return ps, fmt.Errorf("loadPages.loadPage: %w", err)
		}
		ps = append(ps, p)
	}
	return ps, nil
}

func main() {
	http.HandleFunc("/page/", makePageHandlerFunc())
	http.HandleFunc("/api/", makeHandleAPIHandlerFunc())
	http.HandleFunc("/comment/", makeCommentHandlerFunc())
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(*flagFilesFolder))))
	http.HandleFunc("/", makeIndexHandlerFunc())
	fmt.Println("starting server on port", *flagPort)
	err := http.ListenAndServe(":"+*flagPort, nil)
	if err != nil {
		fmt.Println("ListenAndServe:", err)
	}
}

func makeIndexHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	tmpl, err := parseFiles("index.tmpl.html")
	if err != nil {
		panic("makeIndexHandlerFunc: could not parse page.tmpl.html")
	}
	var ps Pages
	go func() {
		for {
			ps, err = loadPages(*flagSrcFolder)
			if err != nil {
				fmt.Println(err)
			}
			fmt.Println("index loaded/")
			time.Sleep(30 * time.Second)
		}
	}()
	return func(w http.ResponseWriter, r *http.Request) {
		err = tmpl.ExecuteTemplate(w, "base", ps)
		if err != nil {
			fmt.Println("MakePageHandlerFunc: tmpl.ExecuteTemplate: %w", err)
		}
	}
}

func makePageHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	tmpl, err := parseFiles("page.tmpl.html")
	if err != nil {
		panic("makePageHandlerFunc: could not parse page.tmpl.html")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		f := r.URL.Path[len("/page/"):]
		fpath := filepath.Join(*flagSrcFolder, f)
		p, err := loadPage(fpath)
		if err != nil {
			fmt.Println(err)
		}
		err = tmpl.ExecuteTemplate(w, "base", p)
		if err != nil {
			fmt.Println("MakePageHandlerFunc: tmpl.ExecuteTemplate: %w", err)
		}
	}
}

func makeCommentHandlerFunc() http.HandlerFunc {
	var mutex = &sync.Mutex{}
	return func(w http.ResponseWriter, r *http.Request) {
		title := r.URL.Path[len("/comment/"):]
		name := r.FormValue("name")
		comment := r.FormValue("comment")
		c := Comment{Name: name, Comment: comment}
		mutex.Lock()
		cs, err := loadComments(title)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		cs = append(cs, c)
		err = saveComments(title, cs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		mutex.Unlock()
		http.Redirect(w, r, "/page/"+title, http.StatusFound)
	}
}

func makeHandleAPIHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps, err := loadPages(*flagSrcFolder)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(ps)
		if err != nil {
			fmt.Println("cannot encode page to json")
		}
	}
}

func parseFiles(content string) (*template.Template, error) {
	return template.ParseFiles(
		filepath.Join(*flagTmplFolder, "base.tmpl.html"),
		filepath.Join(*flagTmplFolder, "header.tmpl.html"),
		filepath.Join(*flagTmplFolder, "footer.tmpl.html"),
		filepath.Join(*flagTmplFolder, "comment.tmpl.html"),
		filepath.Join(*flagTmplFolder, content),
	)
}

func saveComments(title string, cs []Comment) error {
	fpath := filepath.Join("comments", title+".json")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		return fmt.Errorf("saveComments: %w", err)
	}
	enc := json.NewEncoder(f)
	return enc.Encode(cs)
}

func loadComments(title string) ([]Comment, error) {
	var cs []Comment
	fpath := filepath.Join("comments", title+".json")
	f, err := os.Open(fpath)
	if errors.Is(err, os.ErrNotExist) {
		return cs, nil
	}
	if err != nil {
		return cs, fmt.Errorf("loadComments: %w", err)
	}
	dec := json.NewDecoder(f)
	err = dec.Decode(&cs)
	return cs, err
}
