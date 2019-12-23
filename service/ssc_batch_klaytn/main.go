package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	b64 "encoding/base64"

	"github.com/aws/aws-sdk-go/service/kms"
	l "github.com/aws/aws-sdk-go/service/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/olivere/elastic"
	v4 "github.com/olivere/elastic/aws/v4"
)

var (
	// elasticURL      = os.Getenv("ELASTIC_URL")
	elasticURL      = "https://search-es-six-zunsizmfamv7eawswgdvwmyd6u.ap-southeast-1.es.amazonaws.com"
	lambdaFunction  = "SixEchoFunction-ContractClient-1L7MXI5A1UIHC"
	lambdaGetWallet = "SixEchoFunction-GenerateWallet-10O7YCV3G6VM4"
	ctx             = context.Background()
	region          = os.Getenv("AWS_REGION")
	cred            = credentials.NewEnvCredentials()
	signingClient   = v4.NewV4SigningClient(cred, region)
	// sess            = session.Must(session.NewSession())
	sess = session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String("ap-southeast-1"),
		},
		// Profile: "default",
	}))
	client, _ = elastic.NewClient(elastic.SetURL(elasticURL), elastic.SetSniff(false),
		elastic.SetHealthcheck(false),
		// elastic.SetHttpClient(signingClient),
		elastic.SetErrorLog(log.New(os.Stderr, "", log.LstdFlags)), elastic.SetInfoLog(log.New(os.Stdout, "", log.LstdFlags)))
	kmsKeyName = "klaytn-kms"

	svc   = kms.New(sess)
	keyID = "alias/" + kmsKeyName
)

func queryTransactoin() []*Transaction {
	query := elastic.NewBoolQuery().Must(elastic.NewTermQuery("klaytn_tx_id", ""), elastic.NewTermQuery("transaction_action", "create"))
	response, err := client.Search(TransactionAlias).Query(query).Sort("created_at", true).Size(30).Do(context.Background())
	if err != nil {
		panic(err.Error())
	}
	var transactions []*Transaction
	for _, hit := range response.Hits.Hits {
		transaction := Transaction{}
		data, err := hit.Source.MarshalJSON()
		if err != nil {
			panic(err.Error())
		}
		err = json.Unmarshal(data, &transaction)
		transaction.ID = hit.Id
		transactions = append(transactions, &transaction)
	}
	return transactions
}

func getWallets(number int64) []AccountKlaytn {
	payload := map[string]int64{
		"number": number,
	}
	payloadJSON, _ := json.Marshal(payload)
	lambdaClient := l.New(sess)
	input := &l.InvokeInput{
		FunctionName: aws.String(lambdaGetWallet),
		Payload:      payloadJSON,
	}
	result, err := lambdaClient.Invoke(input)
	if err != nil {
		fmt.Println(err.Error())
	}
	var accounts []AccountKlaytn
	err = json.Unmarshal(result.Payload, &accounts)
	if err != nil {
		fmt.Println(err.Error())
	}
	return accounts
}

func submitToKlaytn(mapAccountTxs []MapAccountTx) ResponseKlatyn {
	var kReqs []KlaytnBody
	for _, t := range mapAccountTxs {
		tmp := KlaytnBody{
			Hash:        t.Transaction.ID,
			BlockNumber: fmt.Sprintf("%d", t.Transaction.BlockNumber),
			Account:     t.Account.ID,
			PrivateKey:  t.Account.PrivateKey,
		}
		kReqs = append(kReqs, tmp)
	}
	payload := RequestKlaytn{
		Name: "new-assets",
		Body: kReqs,
	}
	payloadJSON, _ := json.Marshal(payload)
	lambdaClient := l.New(sess)
	input := &l.InvokeInput{
		FunctionName: aws.String(lambdaFunction),
		Payload:      payloadJSON,
	}
	result, err := lambdaClient.Invoke(input)
	if err != nil {
		panic(err.Error())
	}

	var response ResponseKlatyn
	// fmt.Println("@@@@@@@@@@@@@@@@@@@")
	// fmt.Println(string(payloadJSON))
	// fmt.Println(string(result.Payload))
	err = json.Unmarshal(result.Payload, &response)
	if err != nil {
		panic(err.Error)
	}
	return response
}

func matching(txs []MapAccountTx, klaynTxs []Body) []Transaction {
	var tmp []Transaction
	for index, tx := range txs {
		if klaynTxs[index].TransactionHash != "" {
			tx.Transaction.KlaytnTxID = klaynTxs[index].TransactionHash
			tmp = append(tmp, tx.Transaction)
		} else {
			fmt.Println("Error can not submit klaytn EOS Tx ID" + tx.Transaction.ID)
		}
	}
	// data, _ := json.Marshal(tmp)
	// fmt.Println(string(data))
	return tmp
}

func updateElastBatch(txs []Transaction) {
	if len(txs) == 0 {
		return
	}
	bulk := client.Bulk()
	for _, tx := range txs {
		req := elastic.NewBulkUpdateRequest()
		req.Index(TransactionAlias)
		req.Doc(map[string]interface{}{"klaytn_tx_id": tx.KlaytnTxID})
		req.Id(tx.ID)
		req.Type("_doc")
		bulk = bulk.Add(req)
	}
	bulkResp, err := bulk.Refresh("wait_for").Do(ctx)
	if err != nil {
		// panic(err.Error())
		fmt.Println(err.Error())
	}
	// time.Sleep(time.Second * 3)
	fmt.Println(bulkResp.Took)
}

func backGround() {
	fmt.Println("Start...")
	for range time.Tick(time.Second * 2) {
		allProcess()
	}
}

func filterPlatformRefOwner(txs []*Transaction) [][]string {
	unique := make(map[string]bool)
	var result [][]string
	for _, tx := range txs {
		if tx.ToUser.RefOwner != "" {
			if !unique[fmt.Sprintf("%s_%s", tx.Platform, tx.ToUser.RefOwner)] {
				result = append(result, []string{tx.Platform, tx.ToUser.RefOwner})
				unique[fmt.Sprintf("%s_%s", tx.Platform, tx.ToUser.RefOwner)] = true
			}
		}
	}
	return result
}

func mapAccounts(txs []*Transaction) []MapAccountTx {
	query := elastic.NewBoolQuery()

	refOwners := filterPlatformRefOwner(txs)
	for _, ele := range refOwners {
		querySub := elastic.NewBoolQuery()
		querySub.Must(elastic.NewTermQuery("platform", ele[0]), elastic.NewTermQuery("ref_owner", ele[1]))
		query.Should(querySub)
	}

	// @@@@@@@@@@@@
	// src, err := query.Source()
	// if err != nil {
	// panic(err)
	// }
	// data, err := json.MarshalIndent(src, "", "  ")
	// if err != nil {
	// panic(err)
	// }
	// fmt.Println(string(data))
	// @@@@@@@@@@@@

	response, err := client.Search(AccountAlias).Query(query).Do(context.Background())
	if err != nil {
		panic(err.Error())
	}

	var mapAccountTxs []MapAccountTx

	// Map account and transaction, account from Elasticsearch
	for _, ele := range response.Hits.Hits {
		var account Account
		data, err := ele.Source.MarshalJSON()
		if err != nil {
			panic(err.Error())
		}
		json.Unmarshal(data, &account)
		account.ID = ele.Id
		var deleteIndex []int
		for index, tx := range txs {
			if tx.ToUser.RefOwner == account.RefOwner && tx.Platform == account.Platform {
				d := MapAccountTx{
					Account:     account,
					Transaction: *tx,
				}
				mapAccountTxs = append(mapAccountTxs, d)
				deleteIndex = append(deleteIndex, index)
			}
		}
		txs = removeTxByIndex(txs, deleteIndex)
	}
	// Map account and Transaction, account is default
	var deleteIndex []int
	for index, tx := range txs {
		if tx.ToUser.RefOwner == "" {
			d := MapAccountTx{
				Account:     Account{},
				Transaction: *tx,
			}
			mapAccountTxs = append(mapAccountTxs, d)
			deleteIndex = append(deleteIndex, index)
		}
	}
	txs = removeTxByIndex(txs, deleteIndex)

	// Map account and transaction, account from logic
	accounts := insertAccount(txs)
	for _, account := range accounts {
		for _, tx := range txs {
			if tx.ToUser.RefOwner == account.RefOwner && tx.Platform == account.Platform {
				d := MapAccountTx{
					Account:     account,
					Transaction: *tx,
				}
				mapAccountTxs = append(mapAccountTxs, d)
			}
		}
	}
	return mapAccountTxs
}

func encrypt(text string) string {
	result, err := svc.Encrypt(&kms.EncryptInput{
		KeyId:     aws.String(keyID),
		Plaintext: []byte(text),
	})
	if err != nil {
		panic(err.Error())
	}
	sEnc := b64.StdEncoding.EncodeToString(result.CiphertextBlob)
	return sEnc
}

func insertAccount(txs []*Transaction) []Account {
	refOwners := filterPlatformRefOwner(txs)
	if len(refOwners) == 0 {
		return []Account{}
	}

	var accountKlaytns []AccountKlaytn

	for i := 0; i < 3; i++ {
		tmp := getWallets(int64(len(refOwners)))
		accountKlaytns = append(accountKlaytns, tmp...)
		if len(accountKlaytns) == len(refOwners) {
			break
		} else {
			fmt.Println("GetWallet has Error")
		}
	}

	if len(accountKlaytns) != len(refOwners) {
		panic("Error can not create account writer")
	}

	bulk := client.Bulk()
	var accounts []Account
	for index, platfromOwner := range refOwners {
		//@@@@@@@@@@@@@@@@@
		if accountKlaytns[index].PrivateKey == "" {
			fmt.Printf("%#v\n", accountKlaytns[index])
			panic("Check Eerror")
		}
		//@@@@@@@@@@@@@@@@
		req := elastic.NewBulkIndexRequest()
		req.Index(AccountAlias)
		timeStamp := time.Now()
		account := Account{
			Platform:   platfromOwner[0],
			RefOwner:   platfromOwner[1],
			PrivateKey: encrypt(accountKlaytns[index].PrivateKey),
			CreatedAt:  timeStamp.Format("2006-01-02 15:04:05"),
			UpdatedAt:  timeStamp.Format("2006-01-02 15:04:05"),
		}
		req.Doc(account)
		req.Id(strings.ToLower(accountKlaytns[index].Address))
		req.Type("_doc")
		bulk = bulk.Add(req)
		tmp := account
		tmp.ID = accountKlaytns[index].Address
		accounts = append(accounts, tmp)
	}
	_, err := bulk.Refresh("wait_for").Do(ctx)
	if err != nil {
		fmt.Println(err.Error())
	}
	return accounts
}

// func uniqueNonEmptyElementsOf(s []string) []string {
// unique := make(map[string]bool, len(s))
// us := make([]string, len(unique))
// for _, elem := range s {
// if len(elem) != 0 {
// if !unique[elem] {
// us = append(us, elem)
// unique[elem] = true
// }
// }
// }
// return us
// }

func contains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func removeTxByIndex(txs []*Transaction, deleteIndex []int) []*Transaction {
	var tmp []*Transaction
	for index, ele := range txs {
		if !contains(deleteIndex, index) {
			tmp = append(tmp, ele)
		}
	}
	return tmp
}

func allProcess() {
	txs := queryTransactoin()
	if len(txs) > 0 {
		mapAccountTxs := mapAccounts(txs)
		responseKlatyn := submitToKlaytn(mapAccountTxs)
		if len(responseKlatyn.Body) > 0 {
			mapTxs := matching(mapAccountTxs, responseKlatyn.Body)
			updateElastBatch(mapTxs)
			//doc, _ := json.Marshal(txs)
			//fmt.Println(string(doc))
		} else {
			fmt.Println("Submit Klaytn is null")
			time.Sleep(time.Second * 10)
		}
	}
}

func createIndexElastic() {
	fmt.Println("Load elasticsearch...")
	createSSCAccountIndex(client)
}

func main() {
	createIndexElastic()
	backGround()
}
