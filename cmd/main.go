package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

type Client interface {
	ShareText(ctx context.Context, text string) error
	SharePicture(ctx context.Context, text string, reader io.Reader) error
}

// check API
var _ Client = (*weiboShareClient)(nil)

type weiboShareClient struct {
	client      *http.Client
	accessToken string
	sourceURL   string
}

const (
	Host        = "https://api.weibo.com"
	ShareStatus = Host + "/2/statuses/share.json"

	BingBase          = "https://cn.bing.com"
	LatestBingContent = BingBase + "/HPImageArchive.aspx?format=js&idx=0&n=1&mkt=zh-CN"
)

const (
	Urlencoded = "application/x-www-form-urlencoded"
)

const (
	StatusKey = "status"
	PicKey    = "pic"
)

func (w *weiboShareClient) ShareText(ctx context.Context, text string) error {
	payload := strings.NewReader(StatusKey + "=" + text + w.sourceURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ShareStatus, payload)
	if err != nil {
		return err
	}
	w.setAuth(req)
	req.Header.Add("Content-Type", Urlencoded)
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if err := w.handleResponse(response); err != nil {
		return err
	}
	return nil
}

func (w *weiboShareClient) handleResponse(response *http.Response) error {
	if response.StatusCode == 200 {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	} else {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to share text,response body:%s", body)
	}
}

func (w weiboShareClient) setAuth(req *http.Request) {
	req.Header.Add("Authorization", "OAuth2 "+w.accessToken)
}

func (w *weiboShareClient) SharePicture(ctx context.Context, text string, reader io.Reader) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField(StatusKey, text+w.sourceURL)
	file, err := writer.CreateFormFile(PicKey, "pic")
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	_ = writer.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ShareStatus, body)
	if err != nil {
		return err
	}
	w.setAuth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if err := w.handleResponse(response); err != nil {
		return err
	}
	return nil
}

func NewWeiboShareClient(accessToken string, sourceURL string) *weiboShareClient {
	return &weiboShareClient{
		client:      http.DefaultClient,
		accessToken: accessToken,
		sourceURL:   sourceURL,
	}
}

type BingContent struct {
	Description string
	ImageURL    string
}

func (w *weiboShareClient) getBingContent() (*BingContent, error) {
	response, err := w.client.Get(LatestBingContent)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	// {
	//    "images":[
	//        {
	//            "startdate":"20210606",
	//            "fullstartdate":"202106061600",
	//            "enddate":"20210607",
	//            "url":"/th?id=OHR.BuntingBird_ZH-CN0707942842_1920x1080.jpg&rf=LaDigue_1920x1080.jpg&pid=hp",
	//            "urlbase":"/th?id=OHR.BuntingBird_ZH-CN0707942842",
	//            "copyright":"向日葵上的靛蓝彩旗鸟 (© William Krumpelman/Getty Images)",
	//            "copyrightlink":"https://www.bing.com/search?q=%E9%9D%9B%E8%93%9D%E5%BD%A9%E6%97%97%E9%B8%9F&form=hpcapt&mkt=zh-cn",
	//            "title":"",
	//            "quiz":"/search?q=Bing+homepage+quiz&filters=WQOskey:%22HPQuiz_20210606_BuntingBird%22&FORM=HPQUIZ",
	//            "wp":true,
	//            "hsh":"4e112e2afeb9a77a18d0fb69457781ab",
	//            "drk":1,
	//            "top":1,
	//            "bot":1,
	//            "hs":[
	//
	//            ]
	//        }
	//    ],
	//    "tooltips":{
	//        "loading":"Loading...",
	//        "previous":"Previous image",
	//        "next":"Next image",
	//        "walle":"This image is not available to download as wallpaper.",
	//        "walls":"Download this image. Use of this image is restricted to wallpaper only."
	//    }
	// }
	description := gjson.GetBytes(data, "images.0.copyright").String()
	imageURL := gjson.GetBytes(data, "images.0.url").String()
	return &BingContent{
		Description: description,
		ImageURL:    BingBase + imageURL,
	}, nil
}

func (w *weiboShareClient) shareImageFromBing() error {
	bingContent, err := w.getBingContent()
	if err != nil {
		return err
	}
	response, err := w.client.Get(bingContent.ImageURL)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	reader := bufio.NewReader(response.Body)
	if err := w.SharePicture(context.Background(), bingContent.Description, reader); err != nil {
		return err
	}
	return nil
}

func main() {
	var (
		AccessToken = flag.String("TOKEN", "", "the access_token of sina weibo via OAuth 2.0")
		Source      = flag.String("SOURCE", "", "the source url of appID")
	)

	flag.Parse()
	if *AccessToken == "" {
		log.Fatalln("TOKEN cannot be empty.")
	}

	if *Source == "" {
		log.Fatalln("SOURCE cannot be empty.")
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	client := NewWeiboShareClient(*AccessToken, *Source)
	if err := client.shareImageFromBing(); err != nil {
		log.Fatalln(err)
	} else {
		log.Println("success")
	}
}
