package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"napkey-core/internal/casso"
	"napkey-core/internal/logger"
	"napkey-core/internal/store"
)

type Reconciler struct {
	store *store.Store
	apiKey string
	endpoint string
	client *http.Client
}

func NewReconciler(st *store.Store,apiKey string)*Reconciler{
	return &Reconciler{store:st,apiKey:apiKey,endpoint:"https://oauth.casso.vn/v2/transactions",client:&http.Client{Timeout:20*time.Second}}
}

func (r *Reconciler) Run(ctx context.Context){
	if r.apiKey==""{return};ticker:=time.NewTicker(15*time.Minute);defer ticker.Stop()
	for{if err:=r.Reconcile(ctx);err!=nil{logger.Warnf("Casso transaction reconciliation failed: %v",err)};select{case<-ctx.Done():return;case<-ticker.C:}}
}

func (r *Reconciler) Reconcile(ctx context.Context)error{
	to:=time.Now().UTC();from:=to.Add(-24*time.Hour)
	for page:=1;page<=100;page++{
		target,err:=url.Parse(r.endpoint);if err!=nil{return err};q:=target.Query();q.Set("fromDate",from.Format("2006-01-02"));q.Set("toDate",to.Format("2006-01-02"));q.Set("pageSize","100");q.Set("page",fmt.Sprint(page));target.RawQuery=q.Encode()
		req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target.String(),nil);if err!=nil{return err};req.Header.Set("Authorization","Apikey "+r.apiKey)
		resp,err:=r.client.Do(req);if err!=nil{return err};raw,readErr:=io.ReadAll(io.LimitReader(resp.Body,4<<20));resp.Body.Close();if readErr!=nil{return readErr};if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("Casso transactions returned HTTP %d",resp.StatusCode)}
		var envelope struct{Data struct{Records []json.RawMessage `json:"records"`} `json:"data"`};if err:=json.Unmarshal(raw,&envelope);err!=nil{return fmt.Errorf("decoding Casso transactions: %w",err)}
		for _,record:=range envelope.Data.Records{
			wrapped,err:=json.Marshal(map[string]json.RawMessage{"data":record});if err!=nil{return err};tx,err:=casso.ParseTransaction(wrapped);if err!=nil{continue}
			providerID:=tx.ID;if providerID==""{sum:=sha256.Sum256(record);providerID="poll:"+hex.EncodeToString(sum[:])}
			if _,_,err:=r.store.InsertPaymentEvent(ctx,store.PaymentEventInput{ProviderTxID:providerID,BankReference:tx.Reference,SignatureVerified:true,Payload:wrapped,Status:"received"});err!=nil{return err}
		}
		if len(envelope.Data.Records)<100{break}
	}
	return nil
}
