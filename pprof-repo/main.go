package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/pprof/driver"
	"github.com/google/pprof/profile"
)

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "HTTP addr")
	uploadToken := flag.String("upload-token", "pprof", "")
	flag.Parse()

	profilesDir := "profiles"
	if err := os.MkdirAll(profilesDir, 0777); err != nil {
		return err
	}

	handler := &Handler{
		UploadToken: *uploadToken,
		ProfilesDir: profilesDir,
		ProfileMux:  http.NewServeMux(),
	}

	profilePaths, err := filepath.Glob(filepath.Join(profilesDir, "*.pprof"))
	if err != nil {
		return err
	}
	for _, path := range profilePaths {
		if err := handler.LoadProfile(path); err != nil {
			log.Printf("Failed to load profile %s: %v", path, err)
		}
	}

	log.Printf("listening on %s", *addr)
	return http.ListenAndServe(*addr, handler)
}

type ProfileInfo struct {
	Name    string
	ModTime time.Time
}

type Handler struct {
	UploadToken string
	ProfilesDir string
	ProfileMux  *http.ServeMux
	Profiles    []ProfileInfo
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	switch r.URL.Path {
	case "/":
		switch r.Method {
		case http.MethodGet:
			h.index(w, r)
		}
	case "/upload":
		switch r.Method {
		case http.MethodPost:
			h.upload(w, r)
		}
	default:
		if strings.HasPrefix(r.URL.Path, "/profiles/") {
			h.profiles(w, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<html><body>")
	for _, profile := range h.Profiles {
		fmt.Fprintf(w, `<div><a href="/profiles/%s">%s (%s)</a></div>`, profile.Name, profile.Name, profile.ModTime.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "</body></html>\n")
}

var profilesRegexp = regexp.MustCompile(`^/profiles/([a-zA-Z0-9_\-]+)(|/.+)`)

func (h *Handler) profiles(w http.ResponseWriter, r *http.Request) {
	h.ProfileMux.ServeHTTP(w, r)
}

var profileNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	// should be secure compare
	if r.Header.Get("x-upload-token") != h.UploadToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	profileName := r.FormValue("name")
	if !profileNameRegexp.MatchString(profileName) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	profilePath := filepath.Join(h.ProfilesDir, fmt.Sprintf("%s.pprof", profileName))
	f, err := os.OpenFile(profilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	file, _, err := r.FormFile("pprof")
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	_, err = io.Copy(f, file)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := h.LoadProfile(profilePath); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "/profiles/%s\n", profileName)
}

func (h *Handler) LoadProfile(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	profileID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	pprof, err := profile.Parse(file)
	if err != nil {
		return err
	}
	options := &driver.Options{
		Fetch:   &fetcher{pprof: pprof},
		Flagset: new(flagSet),
		UI:      new(ui),
		HTTPServer: func(ha *driver.HTTPServerArgs) error {
			for handlerPath, handler := range ha.Handlers {
				h.ProfileMux.Handle(fmt.Sprintf("/profiles/%s%s", profileID, handlerPath), handler)
			}
			return nil
		},
	}
	if err := driver.PProf(options); err != nil {
		return err
	}

	h.Profiles = append(h.Profiles, ProfileInfo{
		Name:    profileID,
		ModTime: fileInfo.ModTime(),
	})

	sort.Slice(h.Profiles, func(i, j int) bool {
		return h.Profiles[i].ModTime.After(h.Profiles[j].ModTime)
	})

	return nil
}

type fetcher struct {
	pprof *profile.Profile
}

func (f *fetcher) Fetch(src string, duration, timeout time.Duration) (*profile.Profile, string, error) {
	return f.pprof, "", nil
}

type flagSet struct{}

func (s *flagSet) Bool(name string, def bool, usage string) *bool {
	var v bool
	return &v
}
func (s *flagSet) Int(name string, def int, usage string) *int {
	var v int
	return &v
}
func (s *flagSet) Float64(name string, def float64, usage string) *float64 {
	var v float64 = 1
	return &v
}
func (s *flagSet) String(name string, def string, usage string) *string {
	if name == "http" {
		v := "0.0.0.0:0"
		return &v
	}
	var v string
	return &v
}
func (s *flagSet) StringList(name string, def string, usage string) *[]*string {
	var v []*string
	return &v
}
func (s *flagSet) ExtraUsage() string {
	return ""
}

func (s *flagSet) AddExtraUsage(eu string) {
}

func (s *flagSet) Parse(usage func()) []string {
	return []string{"-http", "0.0.0.0:0"}
}

type ui struct{}

func (*ui) ReadLine(prompt string) (string, error)       { return "", nil }
func (*ui) Print(...interface{})                         {}
func (*ui) PrintErr(...interface{})                      {}
func (*ui) IsTerminal() bool                             { return false }
func (*ui) WantBrowser() bool                            { return false }
func (*ui) SetAutoComplete(complete func(string) string) {}
