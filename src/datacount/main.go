package main

//
//                       _oo0oo_
//                      o8888888o
//                      88" . "88
//                      (| -_- |)
//                      0\  =  /0
//                    ___/`---'\___
//                  .' \\|     |// '.
//                 / \\|||  :  |||// \
//                / _||||| -:- |||||- \
//               |   | \\\  -  /// |   |
//               | \_|  ''\---/''  |_/ |
//               \  .-\__  '-'  ___/-. /
//             ___'. .'  /--.--\  `. .'___
//          ."" '<  `.___\_<|>_/___.' >' "".
//         | | :  `- \`.;`\ _ /`;.`/ - ` : | |
//         \  \ `_.   \_ __\ /__ _/   .-` /  /
//     =====`-.____`.___ \_____/___.-`___.-'=====
//                       `=---='
//
//
//     ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
//
//               佛祖保佑         永无BUG
//
//
//
import (
	"github.com/couchbaselabs/go-couchbase"
	"github.com/donnie4w/go-logger/logger"
	zmq "github.com/pebbe/zmq4"
	"io/ioutil"
	j4g "json4g"
	"strconv"
)

const (
	LOGPATH     = "/Users/weijin/log"
	IP          = "tcp://localhost:5557"
	IAQCONFIG   = "./json/iaq.json"
	LEVELCONFIG = "./json/levelconfig.json"
)

var userProductUrl = "http://aicc-UserProductDB:111111@10.10.10.80:8091/"
var couchDataUrl = "http://aicc-CouchbaseDB:111111@10.10.10.80:8091/"

func main() {

	logger.SetConsole(true)
	logger.SetRollingDaily(LOGPATH, "test.log")
	logger.SetLevel(logger.INFO)
	logger.Info("......................【IAQ计算任务开始】......................")
	confgJsonString, err := readFile(IAQCONFIG)
	levelConfgJsonString, levleErr := readFile(LEVELCONFIG)

	if err == nil {
		// 成功读取配置json 构造配置对象
		logger.Info("......................【IAQ配置文件读取完成！】......................")
	} else {
		logger.Error(".....................【IAQ配置文件读取异常】......................")
	}
	if levleErr == nil {
		// 成功读取配置json 构造配置对象
		logger.Info("......................【污染物配置文件读取完成】......................")
	} else {
		logger.Error(".....................【污染物配置文件读取异常】......................")
	}
	//  Socket to receive messages on
	receiver, zmqerr := zmq.NewSocket(zmq.PULL)
	if zmqerr != nil {
		receiver.Close()
		logger.Error(".....................【  创建receiver失败   】....................")
	}

	zmqerr = receiver.Connect(IP)
	if zmqerr != nil {
		logger.Error(".....................【  连接服务器失败】....................")
	} else {
		logger.Info("......................【服务器连接成功,server address=】.........", IP)
	}

	node, err := j4g.LoadByString(confgJsonString)
	if err != nil {
		logger.Error(".....................【构造IAQ配置json失败】.....................")
	}
	levelConfigNode, levelConfigErr := j4g.LoadByString(levelConfgJsonString)
	if levelConfigErr != nil {
		logger.Error(".....................【构造污染物级别json失败】.....................")
	}
	pm25Array := node.GetNodeByPath("pm25")
	hchoArray := node.GetNodeByPath("hcho")
	pm25ConfigArray := levelConfigNode.GetNodeByPath("pm25")
	vocConfigArray := levelConfigNode.GetNodeByPath("voc")
	smokeConfigArray := levelConfigNode.GetNodeByPath("smoke")

	logger.Info("pm2.5 计算标准【", pm25Array.ToString(), "】")        // 转化为字符串
	logger.Info("hcho  计算标准【", hchoArray.ToString(), "】")        // 转化为字符串
	logger.Info("pm2.5 污染级别【", pm25ConfigArray.ToString(), "】")  // 转化为字符串
	logger.Info("voc   污染级别【", vocConfigArray.ToString(), "】")   // 转化为字符串
	logger.Info("smoke 污染级别【", smokeConfigArray.ToString(), "】") // 转化为字符串
	//  Process tasks forever

	for {
		var iaqResult int = 0

		s := `{"datetime":"2014_12_27_12_03_56","sn":"AISN145010011004","pm25":"10","voc":"0.01","smoke":"820","noise":"73","temp":"22","humidity":"28"}`
		//s, _ := receiver.Recv(0)

		dataNode, _ := j4g.LoadByString(s)
		pm25 := dataNode.GetNodeByName("pm25").ValueString
		hcho := dataNode.GetNodeByName("voc").ValueString
		smoke := dataNode.GetNodeByName("smoke").ValueString
		sn := dataNode.GetNodeByName("sn").ValueString
		datetime := dataNode.GetNodeByName("datetime").ValueString

		logger.Info("【编号", sn, "数据信息】pm25=", pm25, "hcho=", hcho, "smoke=", smoke, "datetime=", datetime)
		pm25Int, _ := strconv.Atoi(pm25)
		hchoInt, _ := strconv.ParseFloat(hcho, 64)
		smokeInt, _ := strconv.Atoi(smoke)
		pm25cmin, pm25cmax, pm25imin, pm25imax := iaqConfig(pm25Int, pm25Array)
		hchocmin, hchocmax, hchoimin, hchoimax := iaqConfig(int(hchoInt*1000), hchoArray)
		iAQIpm25 := computeIAQ(pm25Int, pm25cmin, pm25cmax, pm25imin, pm25imax)
		iAQIhcho := computeIAQ(int(hchoInt*1000), hchocmin, hchocmax, hchoimin, hchoimax)
		logger.Info("..................【pm25IAQ 结算结果", iAQIpm25, "】,【HCOIAQ 结算结果", iAQIhcho, "】")
		if iAQIpm25 > iAQIhcho {
			iaqResult = iAQIpm25
		} else {
			iaqResult = iAQIhcho
		}
		logger.Debug("..................【经过比较之后最终的iaq结果：", iAQIpm25, "】..........................")

		setIAQToDB(sn, iaqResult, sn+"_"+datetime) //计算iaq 保存到检测数据桶
		// 计算净化器是否开启的逻辑，并返回完整userProductJson 包含 levelResult
		upNode := openCleanerCompute(sn, levelConfigNode, pm25Int, int(hchoInt*1000), smokeInt, sn+"_"+datetime)
		openCleaner(sn, upNode)
		break
	}

}

// 读取iaq 计算配置文件
func readFile(iaqpath string) (string, error) {
	bytes, err := ioutil.ReadFile(iaqpath)
	if err != nil {
		logger.Error("ReadFile: ", err.Error())
		return "", err
	}
	return string(bytes), nil
}

//根据污染物数值查询计算配置的4个参数
func iaqConfig(pollutant int, node *j4g.JsonNode) (int, int, int, int) {
	arrayNode := node.ArraysStruct
	var cmin int = 0
	var cmax int = 0
	var imin int = 0
	var imax int = 0
	for i := 0; i < len(arrayNode); i++ {
		cmin = int(arrayNode[i].ToJsonNode().GetNodeByName("cmin").ValueNumber)
		cmax = int(arrayNode[i].ToJsonNode().GetNodeByName("cmax").ValueNumber)
		imin = int(arrayNode[i].ToJsonNode().GetNodeByName("imin").ValueNumber)
		imax = int(arrayNode[i].ToJsonNode().GetNodeByName("imax").ValueNumber)
		if pollutant >= int(cmin) && pollutant < int(cmax) {
			logger.Debug("计算公式【(imax-imin)/(cmax-cmin) *(pollint-cmin)+imin】【检测数值=", pollutant, "的计算方法", "(", imax, "-", imin, ")/(", cmax, "-", cmin, ")*(", pollutant, "-", cmin, ")+", cmin)
			return cmin, cmax, imin, imax
			break
		}
		if pollutant >= cmax && i == len(arrayNode)-1 {
			logger.Debug("计算公式【(imax-imin)/(cmax-cmin) *(pollint-cmin)+imin】【检测数值=：", pollutant, "的计算方法", "(", imax, "-", imin, ")/(", cmax, "-", cmin, ")*(", pollutant, "-", cmin, ")+", cmin)

			return cmin, cmax, imin, imax
		}
	}
	return 0, 0, 0, 0
}

//计算污染物iaq
func computeIAQ(pollutant int, cmin int, cmax int, imin int, imax int) int {
	var temp float64 = 0
	temp = float64(imax-imin) / float64(cmax-cmin)
	result := temp*float64(pollutant-cmin) + float64(imin)
	return int(result)
}

// 保存用户最好空气指数，和连续优良天数,同时更新 检测数据桶中的最新iaq信息
func setIAQToDB(sn string, iaq int, key string) {
	b, err := couchbase.GetBucket(userProductUrl,
		"default", "aicc-UserProductDB")
	d, err := couchbase.GetBucket(couchDataUrl,
		"default", "aicc-CouchbaseDB")
	var userProuctJson string = ""
	var userJson string = ""
	if err == nil {
		upbyte, _ := b.GetRaw(sn)

		userProuctJson = string(upbyte)
		//logger.Info(sn, userProuctJson)
	} else {
		logger.Error("保存iaq db 异常")
	}
	upNode, _ := j4g.LoadByString(userProuctJson)
	mobile := upNode.GetNodeByName("mobile").ValueString

	if mobile != "" {
		logger.Debug("设备【", sn, "】的【用户手机号】"+mobile)
		userbyte, _ := b.GetRaw(mobile)
		userJson = string(userbyte)
		userNode, _ := j4g.LoadByString(userJson)
		oldIaq := userNode.GetNodeByPath("iaq")
		oldIaqCount := userNode.GetNodeByPath("iaqcount")

		if oldIaq == nil { //新增
			logger.Debug("【", mobile, "】没有保存过iaq数据，准备新增")
			iaqNode := new(j4g.JsonNode)
			iaqCountNode := new(j4g.JsonNode)

			iaqNode.Name = "iaq"
			iaqNode.SetValue(int(iaq))

			iaqCountNode.Name = "iaqcount"
			iaqCountNode.SetValue(1)

			userNode.AddNode(iaqNode)
			userNode.AddNode(iaqCountNode)

			b.SetRaw(mobile, 0, []byte(userNode.ToCouchDBString()))

			logger.Debug("...................用户【", mobile, "】新增iaq 信息已经保存")
		} else {
			// 更新iaq 和iaqcount
			if iaq <= int(oldIaq.ValueNumber) {
				//更新最小的iaq
				iaqNode := new(j4g.JsonNode)
				iaqCountNode := new(j4g.JsonNode)

				iaqNode.Name = "iaq"
				iaqNode.SetValue(int(iaq))

				iaqCountNode.Name = "iaqcount"

				userNode.AddNode(iaqNode)

				if iaq < 100 {
					iaqCountNode.SetValue(int(oldIaqCount.ValueNumber) + 1)
					userNode.AddNode(iaqCountNode)
				} else {
					iaqCountNode.SetValue(0)
					userNode.AddNode(iaqCountNode)
				}
				b.SetRaw(mobile, 0, []byte(userNode.ToCouchDBString()))
				logger.Debug("...................更新用户室内最好IAQ【", mobile, "】oldiaq=", int(oldIaq.ValueNumber), " newiaq=", iaq, "oldIaqCount=", int(oldIaqCount.ValueNumber), "new iaqCount=", int(oldIaqCount.ValueNumber)+1)

			} else {
				//空气质量变差，从新计数
				iaqCountNode := new(j4g.JsonNode)
				iaqCountNode.Name = "iaqcount"
				iaqCountNode.SetValue(0)
				if iaq < 100 {
					iaqCountNode.SetValue(int(oldIaqCount.ValueNumber) + 1)
					userNode.AddNode(iaqCountNode)
				} else {
					iaqCountNode.SetValue(0)
					userNode.AddNode(iaqCountNode)
				}
				b.SetRaw(mobile, 0, []byte(userNode.ToCouchDBString()))
				logger.Debug("...................【 用户 ", mobile, "空气质量变差重新开始累计】.......................")
			}
		}
		//把iaq更新到 couchdb中

		dataByte, _ := d.GetRaw(key)
		dataJson := string(dataByte)
		dataNode, _ := j4g.LoadByString(dataJson)
		iaqNode := new(j4g.JsonNode)
		iaqNode.Name = "iaq"
		iaqNode.SetValue(int(iaq))
		dataNode.AddNode(iaqNode)
		logger.Debug(".......【带有最新IAQ的检测数据", key, "】-->", dataNode.ToCouchDBString())

		d.SetRaw(key, 0, []byte(dataNode.ToCouchDBString()))
	}
	defer func() {
		if er := recover(); er != nil {
			logger.Error(".............发生严重错误............................")
			logger.Error(er)
			logger.Error(".............发生严重错误............................")

		}
		b.Close()
		d.Close()
	}()
}

// 根据传感器数据计算 传感器污染次数和污染级别，判断是否开启净化器，风力大小 pm25=140,voc=30,smoke=820
func openCleanerCompute(sn string, node *j4g.JsonNode, pm25 int, voc int, smoke int, dataKey string) *j4g.JsonNode {
	pm25LevelArray := node.GetNodeByPath("pm25").ArraysStruct
	vocLevelArray := node.GetNodeByPath("voc").ArraysStruct
	smokeLevelArray := node.GetNodeByPath("smoke").ArraysStruct

	levelTemp := new(j4g.JsonNode)
	var upNode *j4g.JsonNode
	pm25LevelTemp := new(j4g.JsonNode)
	vocLevelTemp := new(j4g.JsonNode)
	smokeLevelTemp := new(j4g.JsonNode)

	levelTemp.Name = "levelResult"
	pm25LevelTemp.Name = "pm25"
	vocLevelTemp.Name = "voc"
	smokeLevelTemp.Name = "smoke"

	// 分别做3种污染的3种级别循环，找出每种污染物的实时值对应的级别
	for i := 0; i < len(pm25LevelArray); i++ {
		cmin := int(pm25LevelArray[i].ToJsonNode().GetNodeByName("cmin").ValueNumber)
		cmax := int(pm25LevelArray[i].ToJsonNode().GetNodeByName("cmax").ValueNumber)
		if pm25 >= cmin && pm25 < cmax {
			// 在一个区间内超标
			pm25LevelTemp.AddNode(j4g.NowJsonNode("name", "pm25"))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("level", float64(i+1)))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else if pm25 > cmax && i != len(pm25LevelArray)-1 {
			// 判断下一个区间
			continue
		} else if pm25 > cmax {
			//pm25 数值超过最严重的标准
			pm25LevelTemp.AddNode(j4g.NowJsonNode("name", "pm25"))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("level", float64(3)))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else {
			//pm25 不超标
			pm25LevelTemp.AddNode(j4g.NowJsonNode("name", "pm25"))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("level", float64(0)))
			pm25LevelTemp.AddNode(j4g.NowJsonNode("count", float64(0)))
			break
		}
	}
	levelTemp.AddNode(pm25LevelTemp)
	for i := 0; i < len(vocLevelArray); i++ {
		cmin := int(vocLevelArray[i].ToJsonNode().GetNodeByName("cmin").ValueNumber)
		cmax := int(vocLevelArray[i].ToJsonNode().GetNodeByName("cmax").ValueNumber)
		if voc >= cmin && voc < cmax {
			// 在一个区间内超标
			vocLevelTemp.AddNode(j4g.NowJsonNode("name", "voc"))
			vocLevelTemp.AddNode(j4g.NowJsonNode("level", float64(i+1)))
			vocLevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else if voc > cmax && i != len(vocLevelArray)-1 {
			// 判断下一个区间
			continue
		} else if voc > cmax {
			//voc 数值超过最严重的标准
			vocLevelTemp.AddNode(j4g.NowJsonNode("name", "voc"))
			vocLevelTemp.AddNode(j4g.NowJsonNode("level", float64(3)))
			vocLevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else {
			//voc 不超标
			vocLevelTemp.AddNode(j4g.NowJsonNode("name", "voc"))
			vocLevelTemp.AddNode(j4g.NowJsonNode("level", float64(0)))
			vocLevelTemp.AddNode(j4g.NowJsonNode("count", float64(0)))
			break
		}
	}
	levelTemp.AddNode(vocLevelTemp)
	for i := 0; i < len(smokeLevelArray); i++ {
		cmin := int(smokeLevelArray[i].ToJsonNode().GetNodeByName("cmin").ValueNumber)
		cmax := int(smokeLevelArray[i].ToJsonNode().GetNodeByName("cmax").ValueNumber)
		if smoke >= cmin && smoke < cmax {
			// 在一个区间内超标
			smokeLevelTemp.AddNode(j4g.NowJsonNode("name", "smoke"))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("level", float64(i+1)))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else if smoke > cmax && i != len(smokeLevelArray)-1 {
			// 判断下一个区间
			continue
		} else if smoke > cmax {
			//smoke 数值超过最严重的标准
			smokeLevelTemp.AddNode(j4g.NowJsonNode("name", "smoke"))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("level", float64(3)))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("count", float64(1)))
			break
		} else {
			//smoke 不超标
			smokeLevelTemp.AddNode(j4g.NowJsonNode("name", "smoke"))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("level", float64(0)))
			smokeLevelTemp.AddNode(j4g.NowJsonNode("count", float64(0)))
			break
		}
	}
	levelTemp.AddNode(smokeLevelTemp)
	logger.Info("检测数据【", dataKey, "】pm25 传感器污染级别为", levelTemp.GetNodeByPath("pm25").GetNodeByName("level").ValueNumber)
	logger.Info("检测数据【", dataKey, "】voc 传感器污染级别为", levelTemp.GetNodeByPath("voc").GetNodeByName("level").ValueNumber)
	logger.Info("检测数据【", dataKey, "】smoke 传感器污染级别为", levelTemp.GetNodeByPath("smoke").GetNodeByName("level").ValueNumber)

	logger.Debug("传感器levelTemp", levelTemp.GetNodeByPath("pm25").GetNodeByName("name").ValueString)

	//开始读取数据库中的设备污染计数器
	b, err := couchbase.GetBucket(userProductUrl,
		"default", "aicc-UserProductDB")
	var userProuctJson string = ""
	if err == nil {
		upbyte, _ := b.GetRaw(sn)

		userProuctJson = string(upbyte)
	} else {
		logger.Error("查询用户设备 异常")
	}
	upNode, _ = j4g.LoadByString(userProuctJson)
	level := upNode.GetNodeByName("levelResult")
	if level == nil {
		logger.Debug("...................【设备第一次进行计算】.................")

		// 当前设备第一次进行计数判断，直接保存计数结果
		for i := 1; i < 4; i++ {
			if int(levelTemp.GetNodeByPath("pm25").GetNodeByName("level").ValueNumber) == 0 {
				levelTemp.GetNodeByPath("pm25").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))
				continue
			} else if int(levelTemp.GetNodeByPath("pm25").GetNodeByName("level").ValueNumber) == i {
				levelTemp.GetNodeByPath("pm25").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(1)))
			} else {
				levelTemp.GetNodeByPath("pm25").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))
			}
		}
		for i := 1; i < 4; i++ {

			if int(levelTemp.GetNodeByPath("voc").GetNodeByName("level").ValueNumber) == 0 {
				levelTemp.GetNodeByPath("voc").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))
				continue
			} else if int(levelTemp.GetNodeByPath("voc").GetNodeByName("level").ValueNumber) == i {
				levelTemp.GetNodeByPath("voc").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(1)))
			} else {
				levelTemp.GetNodeByPath("voc").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))
			}
		}
		for i := 1; i < 4; i++ {

			if int(levelTemp.GetNodeByPath("smoke").GetNodeByName("level").ValueNumber) == 0 {
				levelTemp.GetNodeByPath("smoke").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))
				continue
			} else if int(levelTemp.GetNodeByPath("smoke").GetNodeByName("level").ValueNumber) == i {
				levelTemp.GetNodeByPath("smoke").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(1)))
			} else {
				levelTemp.GetNodeByPath("smoke").AddNode(j4g.NowJsonNode("L"+strconv.Itoa(i), float64(0)))

			}
		}
		levelTemp.Name = "levelResult"
		logger.Info(levelTemp.ToCouchDBString())
		upNode.AddNode(levelTemp)
		logger.Info(upNode.ToCouchDBString())

		logger.Info(upNode.ToCouchDBString())
		b.SetRaw(sn, 0, []byte(upNode.ToCouchDBString()))
		return upNode
	} else {
		// 更新当前设备的计数结果
		logger.Debug("...................【设备更新levelResult】.................")

		for i := 1; i < 4; i++ {
			if int(levelTemp.GetNodeByPath("pm25").GetNodeByName("level").ValueNumber) == 0 {
				level.GetNodeByPath("pm25").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(float64(0))
				continue
			} else if int(levelTemp.GetNodeByPath("pm25").GetNodeByName("level").ValueNumber) == i {
				level.GetNodeByPath("pm25").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(level.GetNodeByPath("pm25").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber + 1)
				break
			}
		}
		for i := 1; i < 4; i++ {

			if int(levelTemp.GetNodeByPath("voc").GetNodeByName("level").ValueNumber) == 0 {
				level.GetNodeByPath("voc").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(float64(0))
				continue
			} else if int(levelTemp.GetNodeByPath("voc").GetNodeByName("level").ValueNumber) == i {
				level.GetNodeByPath("voc").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(level.GetNodeByPath("voc").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber + 1)
				break
			}
		}
		for i := 1; i < 4; i++ {

			if int(levelTemp.GetNodeByPath("smoke").GetNodeByName("level").ValueNumber) == 0 {
				level.GetNodeByPath("smoke").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(float64(0))
				continue
			} else if int(levelTemp.GetNodeByPath("smoke").GetNodeByName("level").ValueNumber) == i {
				level.GetNodeByPath("smoke").GetNodeByPath("L" + strconv.Itoa(i)).SetValue(level.GetNodeByPath("smoke").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber + 1 + 1)
				break
			}
		}
		upNode.AddNode(level)
		b.SetRaw(sn, 0, []byte(upNode.ToCouchDBString()))

		return upNode

	}

	return upNode
}

//根据level 结果 判断是否开启净化器
func openCleaner(sn string, upNode *j4g.JsonNode) {
	var status string = "0"
	var levelInt int = 1
	var count float64 = 0
	level := upNode
	for i := 1; i < 4; i++ {
		count = count + level.GetNodeByPath("levelResult").GetNodeByPath("pm25").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber
		count = count + level.GetNodeByPath("levelResult").GetNodeByPath("voc").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber
		count = count + level.GetNodeByPath("levelResult").GetNodeByPath("smoke").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber
		if level.GetNodeByPath("levelResult").GetNodeByPath("pm25").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber > 0 {
			levelInt = i
			continue
		} else if level.GetNodeByPath("levelResult").GetNodeByPath("voc").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber > 0 {
			levelInt = i
			continue
		} else if level.GetNodeByPath("levelResult").GetNodeByPath("smoke").GetNodeByPath("L"+strconv.Itoa(i)).ValueNumber > 0 {
			levelInt = i
			continue
		} else {
			continue
		}
	}
	if count > 3 {
		status = "1"

	}
	b, err := couchbase.GetBucket(userProductUrl,
		"default", "aicc-UserProductDB")

	cleanArray := upNode.GetNodeByName("cleanSnArray").ArraysStruct
	if len(cleanArray) == 0 {
		logger.Info("设备【", sn, "】没有绑定任何净化器，程序只记录检测数据")
	}
	for i := 0; i < len(cleanArray); i++ {
		cleanSn := cleanArray[i].ToJsonNode().GetNodeByName("sn").ValueString
		if err == nil {
			cleanbyte, _ := b.GetRaw(cleanSn)

			cleanJsonStr := string(cleanbyte)
			cleanNode, _ := j4g.LoadByString(cleanJsonStr)
			if "A" == cleanNode.GetNodeByPath("runType").ValueString {
				cleanNode.GetNodeByPath("status").SetValue(status)
				cleanNode.GetNodeByPath("windpower").SetValue(levelInt)
				b.SetRaw(cleanSn, 0, []byte(cleanNode.ToCouchDBString()))
				logger.Info("【", cleanSn, "】达到自动运行标准开始自动运行", "本次操作 status=", status, "windpower=", levelInt)
			} else {
				logger.Info("【", cleanSn, "】为手动模式，不能自动开启净化器")
			}

		} else {
			logger.Error("查询用户设备 异常")
		}
	}
}
