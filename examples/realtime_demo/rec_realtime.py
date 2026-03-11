# -*- coding:utf-8 -*- 
'''
@author: JM
'''
import tushare as ts
import os
import datetime

"""
// stock, bond, fund
type TsStkBndFnd struct {
 TsCode     string
 Name       string
 TradeTime  string
 PrePrice   string
 Price      string
 Open       string
 High       string
 Low        string
 Close      string
 OpenInt    string
 Volume     string
 Amount     string
 Num        string
 AskPrice1  string
 AskVolume1 string
 BidPrice1  string
 BidVolume1 string
 AskPrice2  string
 AskVolume2 string
 BidPrice2  string
 BidVolume2 string
 AskPrice3  string
 AskVolume3 string
 BidPrice3  string
 BidVolume3 string
 AskPrice4  string
 AskVolume4 string
 BidPrice4  string
 BidVolume4 string
 AskPrice5  string
 AskVolume5 string
 BidPrice5  string
 BidVolume5 string
 
 code 股票代码
name 名称
trade_time 交易时间
pre_price 昨收价
price 现价
open_price 开盘价
high_pirce 最高价
low_price 最低价
close_price 收盘价
open_interst 股票没有这个字段，忽略
volume 成交量
amount 成交额 
num 笔数
ask_price1~5 委卖价1~5 
ask_price1~5 委卖量1~5
bid_price1~5 委买价1~5
bid_volume1~5 委买量1~5
}

//idx 
type TsIdx struct {
 TsCode    string
 Name      string
 TradeTime string
 Price     string
 PreClose  string
 Open      string
 High      string
 Low       string
 Volume    string
 Amount    string
}

//opt
type TsOpt struct {
 TsCode         string
 InstrumentID   string
 TradeTime      string
 PrePrice       string
 Price          string
 Open           string
 High           string
 Low            string
 Close          string
 OpenInt        string
 Volume         string
 Amount         string
 Num            string
 AskPrice1      string
 AskVolume1     string
 BidPrice1      string
 BidVolume1     string
 PreDelta       string
 CurrDelta      string
 DifPrice1      string
 DifPrice2      string
 HighLimitPrice string
 LowLimitPrice  string
 ReferPrice     string
}



// 分钟数据
type TsMin struct {
 TsCode       string
 Freq         string
 TradeTime    string
 Open         string
 Close        string
 High         string
 Low          string
 Volume       string
 Amount       string
 OpenInterest string
}
"""
def subs(token=''):
    if token == '' or token is None:
        token = upass.get_token()

    from tushare.subs.ts_subs.subscribe import TsSubscribe
    app = TsSubscribe(token=token)
    return app

     
def realtime_data():    
    app = ts.subs("a11e32e820d49141b0bcff711d6c4d66dda7e69d228ed0ac20d22750")
    
#     @app.register(topic='HQ_MF_HSGT', codes=['HSGT'])
    @app.register(topic='HQ_STK_TICK', codes=['3*.SZ', '0*.SZ','6*.SH']) #表示订阅全市场
    # @app.register(topic='HQ_STK_TICK', codes=['6*.SH']) #表示订阅上交所全市场
#     @app.register(topic='HQ_STK_TICK', codes=['123156.SZ']) #表示订阅创业板全市场
#    @app.register(topic='HQ_STK_MIN', codes=['1MIN:600*.SH']) #分钟
    
    def data_back(record):
#         data = '%s,%s\n'%(record[0], record[3])
#         file.write(data)
        #在这里处理数据
        print(record)
    
    app.run()

if __name__ == '__main__':
    realtime_data()
#     print(ts.get_token())
