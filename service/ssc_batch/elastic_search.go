package main

import (
	"encoding/json"
	"fmt"

	"github.com/eoscanada/eos-go"
	"github.com/olivere/elastic"
)

func getCurrentBlockNumFromES(client *elastic.Client, blockNumber uint32) uint32 {
	doc, err := client.Get().Index(BlockNumAlias).Type("_doc").Id("1").Pretty(true).Do(ctx)
	if err != nil {
		doc := map[string]interface{}{
			"block_num": blockNumber,
		}
		docJSON, _ := json.Marshal(doc)
		_, err := client.Index().Index(BlockNumAlias).Type("_doc").Id("1").BodyString(string(docJSON)).Do(ctx)
		if err != nil {
			// panic(err.Error())
			fmt.Println("Get Current BlockNum From ES" + err.Error())
		}
		return blockNumber
	}
	data, _ := doc.Source.MarshalJSON()
	type BlockNum struct {
		BlockNum int64 `json:"block_num"`
	}
	var num BlockNum
	json.Unmarshal(data, &num)
	blockNumber = uint32(num.BlockNum)
	return blockNumber
}

func insertTxToES(blockResp *eos.BlockResp, tx eos.TransactionReceipt, action *eos.Action, assetID string, iData *IData, klaytnTxID string, fromto FromToTransaction, detailvalue *string) {
	type Authorization struct {
		Actor      string `json:"actor"`
		Permission string `json:"permission"`
	}
	type DigitalContent struct {
		AssetID           string          `json:"asset_id"`
		AssetType         string          `json:"asset_type"`
		BlockNum          int64           `json:"block_num"`
		KlaytnTxID        string          `json:"klaytn_tx_id"`
		TransactionAction string          `json:"transaction_action"`
		TransactionStatus string          `json:"transaction_status"`
		Authorization     []Authorization `json:"authorization"`
		DetailValues      *string         `json:"detail_values,omitempty"`
		FromToTransaction
		CreatedTime int64  `json:"created_time"`
		UpdatedTime int64  `json:"updated_time"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}

	var authorizationStucts []Authorization
	var authorizations []eos.PermissionLevel
	authorizations = action.Authorization
	for _, ele := range authorizations {
		tmp := Authorization{
			Actor:      string(ele.Actor),
			Permission: string(ele.Permission),
		}
		authorizationStucts = append(authorizationStucts, tmp)
	}
	var assetType string
	assetType = iData.Type
	if assetType == "" {
		assetType = getAssetType(assetID)
	}

	timeStamp := blockResp.Timestamp.Time
	digitalContent := DigitalContent{
		AssetID:           assetID,
		AssetType:         assetType,
		KlaytnTxID:        klaytnTxID,
		BlockNum:          int64(blockResp.BlockNum),
		TransactionAction: string(action.Name),
		TransactionStatus: tx.Status.String(),
		Authorization:     authorizationStucts,
		FromToTransaction: fromto,
		DetailValues:      detailvalue,
		CreatedTime:       timeStamp.Unix(),
		UpdatedTime:       timeStamp.Unix(),
		CreatedAt:         timeStamp.Format("2006-01-02 15:04:05"),
		UpdatedAt:         timeStamp.Format("2006-01-02 15:04:05"),
	}
	digitalContentJSON, _ := json.Marshal(digitalContent)
	_, err := client.Index().Index(TransactionAlias).Type("_doc").Id(tx.Transaction.ID.String()).BodyString(string(digitalContentJSON)).Do(ctx)
	if err != nil {
		fmt.Println("Error insert transaction to ES")
		insertError(blockResp.BlockNum, TXCreateError, err.Error())
		// panic(err.Error())
	}
}

func insertAssetToES(blockResp *eos.BlockResp) {
	for _, tx := range blockResp.Transactions {
		iData := IData{}
		if tx.Transaction.Packed == nil {
			continue
		}
		data, _ := tx.Transaction.Packed.Unpack()
		if len(data.Transaction.Actions) != 0 {
			for _, action := range data.Transaction.Actions {
				//klaytnTxID := submitToKlaytn(tx.Transaction.ID.String(), blockResp.BlockNum)
				klaytnTxID := ""
				if action.Account == "assets" && action.Name == "create" {
					sscData := action.Data.(*SSCDataCreate)
					json.Unmarshal([]byte(sscData.IData), &iData)
					switch typeAsset := iData.Type; typeAsset {
					case "IMAGE":
						go insertImageToES(blockResp, sscData, &iData)
					case "TEXT":
						go insertTextToES(blockResp, sscData, &iData)
					}
					var refInfo *RefInfo
					json.Unmarshal([]byte(sscData.RefInfo), &refInfo)
					fromto := FromToTransaction{
						SubmittedBy: string(sscData.SubmittedBy),
						Platform:    string(sscData.SubmittedBy),
						FromUser:    &refInfo.EchoOwner,
						ToUser:      &refInfo.EchoOwner,
					}

					insertTxToES(blockResp, tx, action, fmt.Sprintf("%d", sscData.AssetID), &iData, klaytnTxID, fromto, nil)

				} else if action.Account == "assets" && action.Name == "transfer" {
					sscDataTransfer := action.Data.(*SSCDataTransfer)
					updateTransferES(blockResp, sscDataTransfer)
					var fromUser, toUser *EchoOwner
					json.Unmarshal([]byte(sscDataTransfer.FromJSONStr), &fromUser)
					json.Unmarshal([]byte(sscDataTransfer.ToJSONStr), &toUser)
					fromto := FromToTransaction{
						FromPlatform: string(sscDataTransfer.From),
						ToPlatform:   string(sscDataTransfer.To),
						FromUser:     fromUser,
						ToUser:       toUser,
						Memo:         sscDataTransfer.Memo,
					}
					insertTxToES(blockResp, tx, action, fmt.Sprintf("%d", sscDataTransfer.AssetID), &iData, klaytnTxID, fromto, nil)
				} else if action.Account == "assets" && action.Name == "setdinfo" {
					sscSetDInfo := action.Data.(*SSCSetDInfo)
					fromto := FromToTransaction{
						Platform: string(sscSetDInfo.Platform),
					}
					assetID := fmt.Sprintf("%d", sscSetDInfo.AssetID)
					setDInfo(blockResp, sscSetDInfo)
					insertTxToES(blockResp, tx, action, assetID, &iData, klaytnTxID, fromto, &sscSetDInfo.DetailInfo)
				} else if action.Account == "assets" && action.Name == "updatecinfo" {
					sscUpdateCInfo := action.Data.(*SSCUpdateCInfo)
					fromto := FromToTransaction{
						Platform: string(sscUpdateCInfo.Platform),
					}
					assetID := fmt.Sprintf("%d", sscUpdateCInfo.AssetID)
					updateCInfo(blockResp, sscUpdateCInfo)
					insertTxToES(blockResp, tx, action, assetID, &iData, klaytnTxID, fromto, &sscUpdateCInfo.DetailInfo)
				} else if action.Account == "assets" && action.Name == "setmdata" {
					sscSetMdata := action.Data.(*SSCSetMdata)
					fromto := FromToTransaction{
						Platform: string(sscSetMdata.Platform),
					}
					assetID := fmt.Sprintf("%d", sscSetMdata.AssetID)
					setMdata(blockResp, sscSetMdata)
					insertTxToES(blockResp, tx, action, assetID, &iData, klaytnTxID, fromto, nil)
				} else if action.Account == "assets" && action.Name == "revoke" {
					sscRevoke := action.Data.(*SSCRevoke)
					fromto := FromToTransaction{
						Platform: string(sscRevoke.Platform),
						Memo:     sscRevoke.Memo,
					}
					assetID := fmt.Sprintf("%d", sscRevoke.AssetID)
					revoke(blockResp, sscRevoke)
					insertTxToES(blockResp, tx, action, assetID, &iData, klaytnTxID, fromto, nil)
				}
			}
		}
	}
}

func insertImageToES(blockResp *eos.BlockResp, sscData *SSCDataCreate, iData *IData) {
	type DataImage struct {
		*IData
		*DetailInfoImage
		*CommonInfo
		*RefInfo
		Revoke      bool   `json:"revoked"`
		Platform    string `json:"platform"`
		SubmittedBy string `json:"submitted_by"`
		MData       string `json:"mdata,omitempty"`
		CreatedTime int64  `json:"created_time"`
		UpdatedTime int64  `json:"updated_time"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	var detailInfo *DetailInfoImage
	var refInfo *RefInfo
	var commonInfo *CommonInfo
	json.Unmarshal([]byte(sscData.DetailInfo), &detailInfo)
	json.Unmarshal([]byte(sscData.RefInfo), &refInfo)
	json.Unmarshal([]byte(sscData.CommonInfo), &commonInfo)
	assetID := fmt.Sprintf("%d", sscData.AssetID)
	dataImage := DataImage{}
	dataImage.IData = iData
	timeStamp := blockResp.Timestamp.Time
	dataImage.DetailInfoImage = detailInfo
	dataImage.RefInfo = refInfo
	dataImage.MData = sscData.MData
	dataImage.CommonInfo = commonInfo
	dataImage.Platform = string(sscData.SubmittedBy)
	dataImage.SubmittedBy = string(sscData.SubmittedBy)
	dataImage.CreatedTime = timeStamp.Unix()
	dataImage.UpdatedTime = timeStamp.Unix()
	dataImage.CreatedAt = timeStamp.Format("2006-01-02 15:04:05")
	dataImage.UpdatedAt = timeStamp.Format("2006-01-02 15:04:05")
	digitalContentJSON, _ := json.Marshal(dataImage)
	_, err := client.Index().Index(ImageAlias).Type("_doc").Id(assetID).BodyString(string(digitalContentJSON)).Do(ctx)
	if err != nil {
		fmt.Println("Error Insert Image To ES : ")
		insertError(blockResp.BlockNum, ImgCreateError, err.Error())
	}
}

func insertTextToES(blockResp *eos.BlockResp, sscData *SSCDataCreate, iData *IData) {
	type DataText struct {
		*IData
		*DetailInfoText
		*CommonInfo
		*RefInfo
		Revoke      bool   `json:"revoked"`
		Platform    string `json:"platform"`
		SubmittedBy string `json:"submitted_by"`
		MData       string `json:"mdata,omitempty"`
		CreatedTime int64  `json:"created_time"`
		UpdatedTime int64  `json:"updated_time"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	var detailInfo *DetailInfoText
	var refInfo *RefInfo
	var commonInfo *CommonInfo
	json.Unmarshal([]byte(sscData.DetailInfo), &detailInfo)
	json.Unmarshal([]byte(sscData.RefInfo), &refInfo)
	json.Unmarshal([]byte(sscData.CommonInfo), &commonInfo)
	dataText := DataText{}
	assetID := fmt.Sprintf("%d", sscData.AssetID)
	timeStamp := blockResp.Timestamp.Time
	dataText.IData = iData
	dataText.DetailInfoText = detailInfo
	dataText.CommonInfo = commonInfo
	dataText.RefInfo = refInfo
	dataText.MData = sscData.MData
	dataText.Platform = string(sscData.SubmittedBy)
	dataText.SubmittedBy = string(sscData.SubmittedBy)
	dataText.CreatedTime = timeStamp.Unix()
	dataText.UpdatedTime = timeStamp.Unix()
	dataText.CreatedAt = timeStamp.Format("2006-01-02 15:04:05")
	dataText.UpdatedAt = timeStamp.Format("2006-01-02 15:04:05")

	digitalContentJSON, _ := json.Marshal(dataText)
	_, err := client.Index().Index(TextAlias).Type("_doc").Id(assetID).BodyString(string(digitalContentJSON)).Do(ctx)
	if err != nil {
		fmt.Println("Error Insert Text To ES : ")
		insertError(blockResp.BlockNum, TextCreateError, err.Error())
	}
}
