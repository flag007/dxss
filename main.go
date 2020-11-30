package main

import (
	"sync"
	"github.com/logrusorgru/aurora"
	"strings"
	"flag"
	"net/url"
	"io/ioutil"
	"bufio"
	"fmt"
	"net/http"
	"os"
	"crypto/tls"
	"net"
	"time"
)

var au aurora.Aurora
var details bool

func init() {
	au = aurora.NewAurora(true)
}

type paramCheck struct {
	url   string
	param string
}

var transport = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: time.Second,
		DualStack: true,
	}).DialContext,
}

var httpClient = &http.Client{
	Transport: transport,
}

var cookie string

var usecookie bool

func main() {

	flag.BoolVar(&details, "v", false, "调试模式")
	flag.BoolVar(&usecookie, "b", false, "使用cookie")


	flag.Parse()
	if usecookie {
		f, err := ioutil.ReadFile("dxss.conf")

		if err != nil {
			fmt.Println("read fail", err)
		}
		cookie = strings.Replace(string(f), "\n", "", -1)
	}

	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	sc := bufio.NewScanner(os.Stdin)

	initialChecks := make(chan paramCheck, 40)


	appendChecks := makePool(initialChecks, func(c paramCheck, output chan paramCheck) {
		reflected,_,err := checkReflected(c.url)
		if err != nil {
			return
		}

		if len(reflected) == 0 {
			return
		}

		for _, param := range reflected {
			output <- paramCheck{c.url, param}
		}

	})

	charChecks := makePool(appendChecks, func(c paramCheck, output chan paramCheck) {
		_, wasReflected,_, err := checkAppend(c.url, c.param, "xxxxxxdkuo")
		if err != nil {
			return
		}

		if wasReflected {
			output <- paramCheck{c.url, c.param}
		}
	})



	done := makePool(charChecks, func(c paramCheck, output chan paramCheck) {
		for _, char := range []string{"\"", "'", "<", "%22", "%27", "%3c"} {
			out, wasReflected, body,err := checkAppend(c.url, c.param, "xxxxxx"+char+"dkuo")
			if err != nil {
				continue
			}

			if wasReflected {
				if details {
					fmt.Printf("[!] %s %s %s\n", au.Yellow(c.param), au.Yellow(char), out)
					fmt.Println(body)
				}else {
					fmt.Printf("[!] %s %s %s\n", au.Yellow(c.param), au.Yellow(char), out)
				}
			}
		}
	})



	for sc.Scan() {
		initialChecks <- paramCheck{url: sc.Text()}
	}

	close(initialChecks)

	<-done

}

func checkAppend(targetURL, param, suffix string) (string, bool, string, error) {
	out := ""
	u, err := url.Parse(targetURL)
	if err != nil {
		return out, false,"", err
	}

	qs := u.Query()
	val := qs.Get(param)

	qs.Set(param, val+suffix)
	u.RawQuery = qs.Encode()


	reflected, body, err := checkReflected(u.String())

	if err != nil {
		return out, false, "", err
	}

	for _, r := range reflected {
		if r == param {
			out = u.String()
			return out, true, body, nil
		}
	}

	return out, false, "", nil
}


func checkReflected(targetURL string) ([]string, string ,error) {
	out := make([]string, 0)
	body := ""
	req, err := http.NewRequest("GET", targetURL, nil)

	if err != nil {
		return out, body, err
	}

	if usecookie{
		req.Header.Add("Cookie", cookie)
	}


	resp, err := httpClient.Do(req)

	if err != nil {
		return out, body, err
	}

	if resp.Body == nil {
		return out, body, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return out, body, err
	}



	//if strings.HasPrefix(resp.Status, "3") {
	//	return out, nil
	//}

	ct := resp.Header.Get("Content-Type")

	if ct != "" && !strings.Contains(ct, "html") {
		return out, body, nil
	}

	u, err := url.Parse(targetURL)

	if err != nil {
		return out, body, err
	}

	body = string(b)

	for key, vv := range u.Query() {
		for _, v := range vv {

			v,_ := url.QueryUnescape(v)

			if !strings.Contains(strings.ToLower(body), strings.ToLower(v)) {
				continue
			}

			out = append(out, key)

		}
	}

	return out, body, nil

}



type workerFunc func(paramCheck, chan paramCheck)

func makePool(input chan paramCheck, fn workerFunc) chan paramCheck {
	var wg sync.WaitGroup

	output := make(chan paramCheck)

	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			for c := range input {
				fn(c, output)
			}

			wg.Done()
		}()

	}

	go func() {
		wg.Wait()
		close(output)
	}()

	return output

}
