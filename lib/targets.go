package stress

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Target is a HTTP request blueprint
type Target struct {
	Method    string
	URL       string
	Body      []byte
	File      string
	Header    http.Header
	OldHeader http.Header
}

// Request creates an *http.Request out of Target and returns it along with an
// error in case of failure.
func (t *Target) Request() (*http.Request, error) {
	// 替换URL中的随机数
	t.URL = string(replaceRI([]byte(t.URL)))
	var req *http.Request
	var err error
	if t.Method == "POST" && t.File != "" {
		// 替换文件名中的随机数
		t.File = string(replaceRI([]byte(t.File)))
		if strings.Contains(t.File, "form") {
			buf := &bytes.Buffer{}
			w := multipart.NewWriter(buf)
			kv := strings.Split(t.File, ":")
			var filekey, filename string
			if len(kv) == 2 {
				filekey = "file"
				filename = kv[1]
			} else if len(kv) == 3 {
				filekey = kv[1]
				filename = kv[2]
			} else {
				return nil, fmt.Errorf("Form file: "+"(%s): illegal", t.File)
			}
			fw, err := w.CreateFormFile(filekey, filename)
			if err != nil {
				//fmt.Println("fail CreateFormFile")
				return nil, err
			}
			fd, err := os.Open(filename)
			if err != nil {
				//fmt.Println("fail Open")
				return nil, err
			}
			defer fd.Close()
			_, err = io.Copy(fw, fd)
			if err != nil {
				//fmt.Println("fail Copy")
				return nil, err
			}
			w.Close()
			req, err = http.NewRequest(t.Method, t.URL, buf)
			req.Header.Set("Content-Type", w.FormDataContentType())
		} else {
			bodyr, err := os.Open(t.File)
			if err != nil {
				return nil, fmt.Errorf("Post file: "+"(%s): %s", t.File, err)
			}
			defer bodyr.Close()
			var body []byte
			if body, err = ioutil.ReadAll(bodyr); err != nil {
				return nil, fmt.Errorf("Post file: "+"(%s): %s", t.File, err)
			}
			req, err = http.NewRequest(t.Method, t.URL, bytes.NewBuffer(body))
			contentLen := len(body)
			req.Header.Set("Content-Length", fmt.Sprint(contentLen))
		}
	} else {
		req, err = http.NewRequest(t.Method, t.URL, bytes.NewBuffer(t.Body))
	}

	if err != nil {
		return nil, err
	}
	for k, vs := range t.Header {
		req.Header[k] = make([]string, len(vs))
		// 替换Header中的随机数
		for i, v := range vs {
			req.Header[k][i] = string(replaceRI([]byte(v)))
		}
		// 使用替换赋值
		// copy(req.Header[k], vs)
	}
	req.Header.Set("User-Agent", "stress 1.0")
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
	}

	// fmt.Printf("--------------t.Header:\n")
	// for k, vs := range t.Header {
	// 	for _, vv := range vs {
	// 		fmt.Printf("%s: %s\n", k, vv)
	// 	}
	// }
	// fmt.Printf("t.Header---------------\n")

	return req, nil
}

// Targets is a slice of Targets which can be shuffled
type Targets []Target

// NewTargetsFrom reads targets out of a line separated source skipping empty lines
// It sets the passed body and http.Header on all targets.
func NewTargetsFrom(source io.Reader, body []byte, header http.Header) (Targets, error) {
	scanner := bufio.NewScanner(source)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()

		if line = strings.TrimSpace(line); line != "" && line[0:2] != "//" {
			// Skipping comments or blank lines
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return NewTargets(lines, body, header)
}

// replaceRI 替换随机数
func replaceRI(b []byte) []byte {
	reg := regexp.MustCompile(`\{RI\[(\d)+-(\d)+\]\}`)
	for {
		if reg.Match(b) {
			// 获取参数
			s := string(b)
			i := strings.Index(s, "{RI[")
			r := s[i+4:]
			j := strings.Index(r, "]}")
			rs := r[:j]
			si := strings.Index(s, "]}")
			ins := strings.Split(rs, "-")
			// TODO handle error
			min, _ := strconv.Atoi(ins[0])
			max, _ := strconv.Atoi(ins[1])
			ran := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
			res := ran.Intn(max-min+1) + min // [min,max]
			rep := strconv.Itoa(res)
			b = []byte(strings.Replace(s, s[i:si+2], rep, -1))
			// b = reg.ReplaceAll(b, rep)
			continue
		}
		return b
	}
}

type headers map[string]string

// NewTargets instantiates Targets from a slice of strings.
// It sets the passed body and http.Header on all targets.
func NewTargets(lines []string, body []byte, header http.Header) (Targets, error) {
	var targets Targets
	for _, line := range lines {
		ps := strings.Split(line, " ")
		argc := len(ps)
		if argc >= 2 {
			newHeader := http.Header{}
			for k, vs := range header {
				newHeader[k] = make([]string, len(vs))
				copy(newHeader[k], vs)
			}
			i := 0
			method := ps[i]
			i++
			if strings.Contains(ps[i], "http") == false {
				for ; i < len(ps) && strings.Contains(ps[i], "http") == false; i++ {
					kv := strings.Split(ps[i], ":")
					if len(kv) != 2 {
						continue
					} else {
						newHeader.Set(kv[0], kv[1])
					}
				}
			}
			var url, postFile string
			if i < argc {
				url = ps[i]
			} else {
				url = ""
			}
			i++
			if i < argc {
				postFile = ps[i]
			} else {
				postFile = ""
			}
			if url != "" {
				targets = append(targets, Target{Method: method, URL: url, File: postFile, Body: body, Header: newHeader})
			}
		} else {
			return nil, fmt.Errorf("Invalid request format: `%s`", line)
		}
	}
	return targets, nil
}

// Shuffle randomly alters the order of Targets with the provided seed
func (t Targets) Shuffle(seed int64) {
	rand.Seed(seed)
	for i, rnd := range rand.Perm(len(t)) {
		t[i], t[rnd] = t[rnd], t[i]
	}
}
