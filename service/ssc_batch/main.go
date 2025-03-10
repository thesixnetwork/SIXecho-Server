package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	l "github.com/aws/aws-sdk-go/service/lambda"
	"github.com/eoscanada/eos-go"
	"github.com/google/uuid"
	"github.com/olivere/elastic"
	v4 "github.com/olivere/elastic/aws/v4"
)

var (
	api *eos.API
	// elasticURL      = os.Getenv("ELASTIC_URL")
	elasticURL      = "https://search-es-six-zunsizmfamv7eawswgdvwmyd6u.ap-southeast-1.es.amazonaws.com"
	eosURL          = "http://3.0.175.142:8888"
	lambdaFunction  = "SixEchoFunction-ContractClient-17IQBE2B7Y5G7"
	ctx             = context.Background()
	currentBlockNum uint32
	blockRunning    uint32
	region          = os.Getenv("AWS_REGION")
	cred            = credentials.NewEnvCredentials()
	signingClient   = v4.NewV4SigningClient(cred, region)
	// sess            = session.Must(session.NewSession())
	sess = session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String("ap-southeast-1"),
		},
	}))
	client, _ = elastic.NewClient(elastic.SetURL(elasticURL), elastic.SetSniff(false),
		elastic.SetHealthcheck(false),
		// elastic.SetHttpClient(signingClient),
		//elastic.SetErrorLog(log.New(os.Stderr, "", log.LstdFlags)),
		//elastic.SetInfoLog(log.New(os.Stdout, "", log.LstdFlags)))
	)
)

func tailBlock(block chan *eos.BlockResp, blockNum chan uint32) {
	for {
		select {
		case num := <-blockNum:
			blockResp, _ := api.GetBlockByNum(num)
			// fmt.Println(blockResp.BlockNum)
			block <- blockResp
		}
	}
}

func updateBlockNumToES() {
	fmt.Println(blockRunning)
	doc := map[string]interface{}{
		//"block_num": currentBlockNum,
		"block_num": blockRunning,
	}
	docJSON, _ := json.Marshal(doc)
	_, err := client.Index().Index(BlockNumAlias).Type("_doc").Id("1").BodyString(string(docJSON)).Do(ctx)
	if err != nil {
		fmt.Println("Update Block Error : " + " " + err.Error())
		// panic(err.Error())
	}
}
func queryAssetID(blockNum uint32) string {
	uid := uuid.New()
	return uid.String()
}
func excuteSSC(blockResp *eos.BlockResp) {
	insertAssetToES(blockResp)
	blockRunning = blockResp.BlockNum
}
func submitToKlaytn(sscTxID string, blockNum uint32) string {

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String("ap-southeast-1"),
		},
	}))
	type KlaytnBody struct {
		Hash        string `json:"hash"`
		BlockNumber string `json:"block_number"`
	}
	type RequestKlaytn struct {
		Name string     `json:"name"`
		Body KlaytnBody `json:"body"`
	}
	payload := RequestKlaytn{
		Name: "new-asset",
		Body: KlaytnBody{
			Hash:        sscTxID,
			BlockNumber: string(int64(blockNum)),
		},
	}
	payloadJSON, _ := json.Marshal(payload)
	lambdaClient := l.New(sess)
	input := &l.InvokeInput{
		FunctionName: aws.String(lambdaFunction),
		Payload:      payloadJSON,
	}
	result, err := lambdaClient.Invoke(input)
	if err != nil {
		fmt.Println("Submit Klaytn Error:")
		panic(err.Error())
	}
	fmt.Println(string(result.Payload))
	var response ResponseKlatyn
	json.Unmarshal(result.Payload, &response)
	return response.Body.TransactionHash
}

func loadAllBackgroundProcess(block chan *eos.BlockResp, blockNum chan uint32) {
	go func() {
		for range time.Tick(time.Second * 5) {
			updateBlockNumToES()
		}
	}()
	go func() {
		for range time.Tick(time.Second * 1) {
			infoResp, err := api.GetInfo()
			if err != nil {
				fmt.Println(err.Error())
			} else {
				lastBlockNum := infoResp.HeadBlockNum
				for i := currentBlockNum; i < lastBlockNum; i++ {
					blockNum <- i
				}
				currentBlockNum = lastBlockNum
			}
		}
	}()
	go func() {
		for {
			select {
			case data := <-block:
				// fmt.Println(reflect.TypeOf(data))
				excuteSSC(data)
			}
		}
	}()
}

func getCurrentBlockNum() {
	infoResp, _ := api.GetInfo()
	currentBlockNum = getCurrentBlockNumFromES(client, infoResp.HeadBlockNum)
}

func createIndexElastic() {
	fmt.Println("Load elasticsearch...")
	createSSCBlockNumIndex(client)
	createSSCDigitalContentIndex(client)
	createSSCImageIndex(client)
	createSSCTextIndex(client)
	createErrorsIndex(client)
}

func createIndexElasticV2() {
	fmt.Println("Load elasticsearch version 2...")
	createSSCBlockNumIndexV2(client)
	createSSCDigitalContentIndexV2(client)
	createSSCImageIndexV2(client)
	createSSCTextIndexV2(client)
	createErrorsIndexV2(client)
}

func main() {
	createIndexElasticV2()
	block := make(chan *eos.BlockResp)
	blockNum := make(chan uint32)
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("create"), SSCDataCreate{})
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("transfer"), SSCDataTransfer{})
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("setmdata"), SSCSetMdata{})
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("setdinfo"), SSCSetDInfo{})
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("updatecinfo"), SSCUpdateCInfo{})
	eos.RegisterAction(eos.AccountName("assets"), eos.ActionName("revoke"), SSCRevoke{})
	api = eos.New(eosURL)
	getCurrentBlockNum()
	loadAllBackgroundProcess(block, blockNum)
	fmt.Println("Running....")
	tailBlock(block, blockNum)
}
