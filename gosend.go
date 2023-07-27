package main;

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
)

type IsDirError struct {
	path string
}

func (d *IsDirError) Error() string {
	return fmt.Sprintf("%s is a directory: recursive uploads are unsupported.", d.path)
}

// HEY YOU: Change these to your personal LPix username and password.
const username = "MayOrMayNotBeACat"
const password = "4hG-EY8-drP-sh7"
const my_mb_limit = 2 // Most LPix users have a 2 MB upload limit. If you have a boosted upload limit, increase this number to your personal limit, in MB.

type TooLargeError struct {
	path string
}

func (t *TooLargeError) Error() string {
	return fmt.Sprintf("%s too big to upload (over %v MB).", t.path, my_mb_limit)
}

var help = flag.Bool("help", false, "show this help message")
var logfilepath = flag.String("log", "", "name of a log `file` to store all resulting [img] codes")
var tlogfilepath = flag.String("tlog", "", "name of a log `file` to store all resulting [timg] codes")
var gallery = flag.String("gallery", "Default", "gallery to store uploaded images in")

var logfile *os.File
var tlogfile *os.File

func logString(str string) {
	if logfile != nil {
		_, err := logfile.WriteString(str)
		if err != nil {
			log.Printf("An error occurred while writing to the [img] log: %v\n", err)
		}
	}
	if tlogfile != nil {
		_, err := tlogfile.WriteString(str)
		if err != nil {
			log.Printf("An error occurred while writing to the [timg] log: %v\n", err)
		}
	}
}

func newFileUploadRequest(uri string, params map[string]string, paramName, path string) (*http.Request, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().IsDir() {
		return nil, &IsDirError{path}
	}
	if fi.Size() > my_mb_limit * 1024 * 1024 {
		return nil, &TooLargeError{path}
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(paramName, fi.Name())
	if err != nil {
		return nil, err
	}
	io.Copy(part, file)

	for key, val := range params {
		_ = writer.WriteField(key, val)
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
 
	request, err := http.NewRequest("POST", uri, body)
	request.Header.Add("Content-Type", writer.FormDataContentType())
	return request, err
}

func main() {
	flag.Parse()
	if *help {
		flag.Usage()
		os.Exit(0)
	}
	if flag.NArg() == 0 {
		flag.Usage()
		log.Fatalln("Error: You need to specify at least one file to upload")
	}
	if *logfilepath != "" {
		l, err := os.OpenFile(*logfilepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Unable to open log for [img] codes at %s:%v\n", *logfilepath, err)
		} else {
			logfile = l
			defer logfile.Close()
		}
	}
	if *tlogfilepath != "" {
		t, err := os.OpenFile(*tlogfilepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Unable to open log for [timg] codes at %s:%v\n", *tlogfilepath, err)
		} else {
			tlogfile = t
			defer tlogfile.Close()
		}
	}
	files := flag.Args()
	client := &http.Client{}
	for _, file := range files {
		request, err := newFileUploadRequest("https://lpix.org/api", map[string]string{
			"username": username,
			"password": password,
			"gallery": *gallery,
			"output": "json",
		}, "file", file)
		if err != nil {
			switch v := err.(type) {
			case *IsDirError:
				log.Println(err)
				log.Printf("Ignoring %s\n", v.path)
			case *TooLargeError:
				log.Println(err)
				logString(fmt.Sprintf("Skipped overlarge file %s\n", v.path))
			default:
				if os.IsNotExist(err) {
					log.Printf("%s not found.\n", file)
					logString(fmt.Sprintf("Couldn't find %s.\n", file))
				} else {
					log.Printf("Couldn't create request for uploading %s.\n", file)
					logString(fmt.Sprintf("Couldn't attempt to upload %s.\n", file))
				}
			}
			continue
		}
		fmt.Printf("Uploading %s.\n", file)
		resp, err := client.Do(request)
		if err != nil {
			log.Println("I sent the file, but no response was received while uploading. You might want to try uploading it again later.")
			logString(fmt.Sprintf("No server response while uploading %s.\n", file))
			continue
		}
		bodyContent, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("I sent the file, but part of the response was lost. You should check to see if the upload succeeded.")
			logString(fmt.Sprintf("Lost server response while uploading %s.\n", file))
			continue
		}
		resp.Body.Close()
		var respObj map[string]string
		err = json.Unmarshal(bodyContent, &respObj)
		if err != nil {
			log.Println("I sent the file, but an invalid response was received. You should check to see if the upload succeeded.")
			log.Printf("JSON error: %v\n", err)
			logString(fmt.Sprintf("Invalid server response received while uploading %s.\n", file))
		}
		errField, errorFieldPresent := respObj["err"]
		if errorFieldPresent && errField != "" {
			var errorExplanation string
			var permanentError bool
			switch errField {
			case "err1":
				errorExplanation = "something went wrong during the upload process itself."
			case "err2":
				errorExplanation = "authentication error. Check your username and password!"
				permanentError = true
			case "err3":
				errorExplanation = "this file doesn't appear to be an image or MP3 file."
			case "err4":
				errorExplanation = "your file is too big."
			case "err5":
				errorExplanation = "at time of writing, this error code is reserved by Baldurk for future use. Maybe post about this on the forums?"
			case "err6":
				errorExplanation = "the server is down for maintenance. Try again later."
				permanentError = true
			default:
				errorExplanation = fmt.Sprintf("unknown error, using the error code \"%s\".", errField)
				permanentError = true
			}
			log.Printf("Received error when uploading %s: %s\n", file, errorExplanation)
			logString(fmt.Sprintf("Couldn't upload file %s.\n", file))
			if permanentError {
				log.Fatalln("Stopping now")
			}
			continue
		}
		fmt.Printf("Image URL: [img]%s[/img]\nThumbnail Code: [timg]%s[/timg]\n", respObj["imageurl"], respObj["thumburl"])
		if logfile != nil {
			_, err := logfile.WriteString(fmt.Sprintf("[img]%s[/img]\n", respObj["imageurl"]))
			if err != nil {
				log.Printf("An error occurred while writing to the [img] log: %v\n", err)
			}
		}
		if tlogfile != nil {
			_, err := tlogfile.WriteString(fmt.Sprintf("[timg]%s[/timg]\n", respObj["thumburl"]))
			if err != nil {
				log.Printf("An error occurred while writing to the [timg] log: %v\n", err)
			}
		}
	}
}