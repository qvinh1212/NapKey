package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

var errWalletInsufficient = errors.New("wallet balance is insufficient")

type billingClient struct {
	baseURL string
	token string
	http *http.Client
}

type billingLease struct {
	RequestID string
	client *billingClient
	consumed atomic.Bool
}

func (l *billingLease) releaseIfUnused(){
	if l==nil||l.consumed.Load(){return};ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();_ = l.client.Release(ctx,l.RequestID)
}

func newBillingClient(baseURL,token string)*billingClient{
	return &billingClient{baseURL:strings.TrimRight(baseURL,"/"),token:token,http:&http.Client{Timeout:10*time.Second}}
}

func (c *billingClient) Reserve(ctx context.Context,keyID,model string,inputTokens,maxOutputTokens int64)(*billingLease,error){
	requestID:="req_"+uuid.New().String()
	body:=map[string]any{"keyId":keyID,"requestId":requestID,"model":model,"inputTokens":inputTokens,"maxOutputTokens":maxOutputTokens}
	for attempt:=0;attempt<3;attempt++{status,err:=c.post(ctx,"/internal/wallet/reserve",body);if err==nil&&status>=200&&status<300{return &billingLease{RequestID:requestID,client:c},nil};if status==http.StatusPaymentRequired{return nil,errWalletInsufficient};if err==nil&&status<500{return nil,fmt.Errorf("wallet reserve returned HTTP %d",status)};if attempt<2{select{case<-ctx.Done():return nil,ctx.Err();case<-time.After(time.Duration(attempt+1)*100*time.Millisecond):}}}
	return nil,errors.New("wallet reserve failed after retries")
}

func (c *billingClient) Release(ctx context.Context,requestID string)error{
	status,err:=c.post(ctx,"/internal/wallet/release",map[string]string{"requestId":requestID});if err!=nil{return err};if status<200||status>=300{return fmt.Errorf("wallet release returned HTTP %d",status)};return nil
}

func (c *billingClient) post(ctx context.Context,path string,body any)(int,error){
	raw,err:=json.Marshal(body);if err!=nil{return 0,err}
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.baseURL+path,bytes.NewReader(raw));if err!=nil{return 0,err}
	req.Header.Set("Content-Type","application/json");req.Header.Set("X-Internal-Token",c.token)
	resp,err:=c.http.Do(req);if err!=nil{return 0,err};defer resp.Body.Close();return resp.StatusCode,nil
}

var billingOnce sync.Once
var globalBillingClient *billingClient

func BillingClient()*billingClient{
	billingOnce.Do(func(){base:=strings.TrimSpace(os.Getenv(envUsageReportURL));token:=strings.TrimSpace(os.Getenv(envUsageReportToken));if base!=""&&token!=""{globalBillingClient=newBillingClient(base,token)}});return globalBillingClient
}

type billingContextKey struct{}

func withBillingLease(r *http.Request,lease *billingLease)*http.Request{return r.WithContext(context.WithValue(r.Context(),billingContextKey{},lease))}
func billingLeaseFromContext(ctx context.Context)*billingLease{lease,_:=ctx.Value(billingContextKey{}).(*billingLease);return lease}

func estimateBillingRequest(r *http.Request)(string,int64,int64,error){
	raw,err:=io.ReadAll(r.Body);if err!=nil{return "",0,0,err};r.Body=io.NopCloser(bytes.NewReader(raw))
	var request struct{Model string `json:"model"`;MaxTokens int64 `json:"max_tokens"`;MaxOutputTokens int64 `json:"max_output_tokens"`};if err:=json.Unmarshal(raw,&request);err!=nil{return "",0,0,err}
	maxOutput:=request.MaxTokens;if maxOutput<=0{maxOutput=request.MaxOutputTokens};if maxOutput<=0{maxOutput=8192}
	input:=int64(len(raw)/3+1024);return request.Model,input,maxOutput,nil
}
