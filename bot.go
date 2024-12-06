package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/boltdb/bolt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gopkg.in/yaml.v2"
)

const (
	maxLogSize    = 10 * 1024 * 1024 // 10MB
	maxLogBackups = 5
)

// filename 存储机器人配置文件的名称
var filename = "bot.map"

// Config 存储机器人的配置信息
type Config struct {
	Account struct {
		Mode     string `yaml:"mode"`     // 工作模式：polling 或 webhook
		Token    string `yaml:"token"`    // Telegram Bot Token
		Owner    int64  `yaml:"owner"`    // 管理员的 Telegram ID
		Endpoint string `yaml:"endpoint"` // webhook 模式的回调地址
		Port     int    `yaml:"port"`     // webhook 模式的端口
	} `yaml:"account"`
}

// BotConfig 存储机器人的配置信息
var BotConfig Config

// bucketname 存储消息ID映射关系的 bucket 名称
var bucketname = []byte("msg2chatid")

// db 存储消息ID映射关系的 BoltDB 实例
var db *bolt.DB

// lastreplyid 存储最后一次回复的消息ID
var lastreplyid int

// bot Telegram Bot API 实例
var bot *tgbotapi.BotAPI

// 设置日志轮转
func setupLogging() (*os.File, error) {
	// 检查日志文件大小
	if fi, err := os.Stat("bot.log"); err == nil {
		if fi.Size() > maxLogSize {
			// 轮转日志文件
			for i := maxLogBackups - 1; i > 0; i-- {
				oldPath := fmt.Sprintf("bot.log.%d", i)
				newPath := fmt.Sprintf("bot.log.%d", i+1)
				if _, err := os.Stat(oldPath); err == nil {
					os.Rename(oldPath, newPath)
				}
			}
			os.Rename("bot.log", "bot.log.1")
		}
	}

	// 打开新的日志文件
	logFile, err := os.OpenFile("bot.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("无法创建日志文件: %v", err)
	}

	// 设置日志格式
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	return logFile, nil
}

func cleanup() {
	if db != nil {
		db.Close()
	}
	os.Remove("bot.db.lock")
	// 清理过期的日志文件
	files, _ := filepath.Glob("bot.log.*")
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			if time.Since(fi.ModTime()) > 30*24*time.Hour { // 删除30天前的日志
				os.Remove(f)
			}
		}
	}
}

func main() {
	// 设置清理函数
	defer cleanup()

	// 捕获信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigChan {
			log.Printf("收到信号: %v, 开始清理...", sig)
			cleanup()
			if sig == syscall.SIGHUP {
				// 重新加载配置
				if err := loadConfig(); err != nil {
					log.Printf("重新加载配置失败: %v", err)
				}
				setupLogging()
			} else {
				os.Exit(0)
			}
		}
	}()

	// 设置日志
	logFile, err := setupLogging()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer logFile.Close()

	// 加载配置
	if err := loadConfig(); err != nil {
		log.Printf("加载配置失败: %v", err)
		return
	}

	// 初始化数据库
	if err := initDB(); err != nil {
		log.Printf("初始化数据库失败: %v", err)
		return
	}

	// 启动机器人
	bot, err = tgbotapi.NewBotAPI(BotConfig.Account.Token)
	if err != nil {
		log.Printf("Failed to create bot: %v", err)
		panic("create bot fail: " + err.Error())
	}
	go InitBot(BotConfig.Account.Mode, BotConfig.Account.Token, BotConfig.Account.Endpoint, BotConfig.Account.Port, handleUpdate)

	// 启动命令行接口
	startCommandLine()
}

func loadConfig() error {
	yamlFile, err := os.ReadFile("bot.yaml")
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	err = yaml.Unmarshal(yamlFile, &BotConfig)
	if err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	return nil
}

func initDB() error {
	// 尝试删除可能存在的锁文件
	os.Remove("bot.db.lock")
	os.Remove("bot.db")

	var err error
	db, err = bolt.Open("bot.db", 0600, &bolt.Options{
		Timeout: 3 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}

	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketname)
		if err != nil {
			return fmt.Errorf("创建消息存储桶失败: %v", err)
		}
		return nil
	})
}

// deliverIncomingMsg 处理接收到的消息
// 将消息转发给管理员并存储消息ID映射关系
func deliverIncomingMsg(msg SimpleMsg) {
	log.Printf("receive message from %d %s\n", msg.ChatId, msg.Name)
	var info string
	if msg.Text != "" {
		info = msg.Text
	} else if msg.FileID != "" {
		info = fmt.Sprintf("file: %s", msg.FileName)
	} else if msg.PhotoID != "" {
		info = fmt.Sprintf("photo: %s", msg.PhotoID)
	} else if msg.VideoID != "" {
		info = fmt.Sprintf("video: %s", msg.VideoID)
	}

	fmt.Printf("(%d)%s: %s\n:: ", msg.ChatId, msg.Name, info)
	lastreplyid = int(msg.ChatId)
	msgid := ForwardMsg(BotConfig.Account.Owner, msg.ChatId, msg.MessageID)
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketname)
		b.Put([]byte(strconv.Itoa(msgid)), []byte(strconv.Itoa(int(msg.ChatId))))
		log.Printf("store chatid %d for message %d\n", msg.ChatId, msgid)
		return nil
	})
	log.Printf("收到消息来自 %d, 消息 id %d, 消息内容 %s\n", msg.ChatId, msgid, info)
}

// directmsg 处理直接发送消息的命令
// 格式：*chatid message
func directmsg(msg SimpleMsg) {
	chatid := int(0)
	for i := 1; i < len(msg.Text); i++ {
		if msg.Text[i] == ' ' {
			chatid, _ = strconv.Atoi(msg.Text[1:i])
			msg.Text = msg.Text[i+1:]
			break
		}
	}
	if chatid == 0 {
		SendMsg(msg.ChatId, "format invaild")
		return
	}
	if msg.Text != "" {
		SendMsg(int64(chatid), msg.Text)
	}
}

// deliverOutgoingMsg 处理发出的消息
// 支持文本、图片、视频和文件的转发
func deliverOutgoingMsg(msg SimpleMsg) {
	if msg.Text != "" && msg.Text[0] == '*' {
		directmsg(msg)
		return
	}
	storechatid := 0
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketname)
		v := b.Get([]byte(strconv.Itoa(msg.ReplyID)))
		if v != nil {
			storechatid, _ = strconv.Atoi(string(v))
		}
		return nil
	})
	if storechatid == 0 || storechatid == int(msg.ChatId) {
		SendMsg(msg.ChatId, "reply to forward ...")
	} else {
		if msg.Text != "" {
			fmt.Printf("(%d)%s\n", storechatid, msg.Text)
			SendMsg(int64(storechatid), msg.Text)
		} else if msg.PhotoID != "" {
			SendExistingPhoto(int64(storechatid), msg.PhotoID)
		} else if msg.VideoID != "" {
			SendExistingVideo(int64(storechatid), msg.VideoID)
		} else if msg.FileID != "" {
			SendExistingFile(int64(storechatid), msg.FileID, msg.FileName)
		}
	}
}

// deliverOutgoingMsgCmdLine 处理命令行接口发出的消息
func deliverOutgoingMsgCmdLine(replyid int, text string) {
	fmt.Printf("(%d)%s\n", replyid, text)
	SendMsg(int64(replyid), text)
}

var welcomeMsg = `*欢迎光临号多多*

1\. 请少量购买测试业务后再批量购买！！！
2\. 本站域名：[hdd\.cm](https://hdd\.cm/),分享频道：[hddinfo](https://t\.me/hddcm\_info/)
3\. 购买前请先查看商品介绍和登录教程
4\. 购买后请及时提取使用
5\. 购买后遇到问题请及时联系客服
6\. 购买成功后，请在24小时内提取使用
7\. 请勿频繁登录，建议一次登录多开标签
8\. 登录成功后请及时修改密码
9\. *auth\_token为第5段，40位小写hex字符串*
10\. *2faCode为第6段，16位大写字符串*
11\. 卡密提取单独token [点击使用](https://zh\.hdd\.cm)
12\. 直接发送消息即可联系人工客服`

var tokenTutorial = `*Auth\_token登录教程*

1\. 安装 Chrome 插件 [点击安装](https://chrome\.google\.com/webstore/detail/twitter\-token\-login/gidagjoldoibifeocgoioiblgpehmbad)
2\. 打开浏览器，进入推特登录页面
3\. 点击插件图标，输入 auth\_token
4\. 点击 login 按钮，等待浏览器跳转
5\. 如果无法点击插件图标，请右键菜单，点击 使用令牌登录 Twitter`

var twoFaTutorial = `*2FA登录教程*

1\. 打开推特登录页面
2\. 输入账号密码
3\. 提示输入代码登录，打开[2fa网站](https://2fa\.hdd\.cm/)
4\. 输入2faCode，页面下方会生成一个6位数字
5\. 返回推特登录页面，输入6位数字，完成登录`

// commander 处理命令
func commander(msg SimpleMsg) {
	if msg.Text == "/start" {
		SendStart(msg.ChatId)
	}
}

func SendStart(chatID int64) {
	markup := tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			{
				{
					Text:         "token登录教程",
					CallbackData: stringPtr("tokenLoginDoc"),
				},
				{
					Text:         "2Fa登录教程",
					CallbackData: stringPtr("2FaLoginDoc"),
				},
			},
		},
	}
	msg := tgbotapi.NewMessage(chatID, welcomeMsg)
	msg.ParseMode = "MarkdownV2" // 改用 MarkdownV2
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = markup
	bot.Send(msg)
}

// handleCallback 处理按钮回调
func handleCallback(callback *tgbotapi.CallbackQuery) {
	if callback == nil {
		log.Println("警告: 收到空回调")
		return
	}

	// 确认收到回调
	msg := tgbotapi.NewCallback(callback.ID, "")
	if _, err := bot.Request(msg); err != nil {
		log.Printf("处理回调请求失败: %v", err)
		return
	}

	var text string
	switch callback.Data {
	case "tokenLoginDoc":
		text = tokenTutorial
		log.Println("发送token登录教程")
	case "2FaLoginDoc":
		text = twoFaTutorial
		log.Println("发送2FA登录教程")
	default:
		log.Printf("未知的回调数据: %s", callback.Data)
		return
	}

	msg2 := tgbotapi.NewMessage(callback.Message.Chat.ID, text)
	msg2.ParseMode = "MarkdownV2"
	msg2.DisableWebPagePreview = true

	if _, err := bot.Send(msg2); err != nil {
		log.Printf("发送教程消息失败: %v", err)
		plainMsg := tgbotapi.NewMessage(callback.Message.Chat.ID, "抱歉，发送教程时出现错误，请稍后重试。")
		bot.Send(plainMsg)
	}
}

// handleUpdate 处理 Telegram 更新事件
func handleUpdate(update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("处理更新时发生错误: %v\n", r)
			SendMsg(BotConfig.Account.Owner, "处理消息时出现错误！请查看日志了解详情。")
			debug.PrintStack()
		}
	}()

	// 处理按钮回调
	if update.CallbackQuery != nil {
		handleCallback(update.CallbackQuery)
		return
	}

	msg := FormatMsg(update)
	if msg.Type != "private" {
		return
	}

	// 处理命令
	if strings.HasPrefix(msg.Text, "/") {
		commander(msg)
		return
	}

	if msg.FromID == BotConfig.Account.Owner {
		deliverOutgoingMsg(msg)
	} else {
		deliverIncomingMsg(msg)
	}
}

// SaveMapToDisk 保存消息ID映射关系到磁盘
func SaveMapToDisk(m map[int]int64) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(m)
	if err != nil {
		return err
	}

	return nil
}

// LoadMapFromDisk 从磁盘加载消息ID映射关系
func LoadMapFromDisk() (map[int]int64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return make(map[int]int64), err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var m map[int]int64
	err = decoder.Decode(&m)
	if err != nil {
		return make(map[int]int64), err
	}
	return m, nil
}

// startCommandLine 启动命令行接口
func startCommandLine() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(":: ")
		text, _ := reader.ReadString('\n')
		doCommand(text)
	}
}

// parseCommand 解析命令
func parseCommand(text string) (string, []string) {
	cmdarr := strings.Split(text, " ")
	cmd := cmdarr[0]
	args := cmdarr[1:]
	return cmd, args
}

// isNumber 判断字符串是否为数字
func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// doCommand 执行命令
func doCommand(text string) {
	if text == "" {
		return
	}
	cmd, args := parseCommand(text)
	if cmd == "!" || cmd == "0" {
		deliverOutgoingMsgCmdLine(lastreplyid, args[0])
	} else if isNumber(cmd) {
		chatid, _ := strconv.Atoi(cmd)
		SendMsg(int64(chatid), args[0])
	} else {
		fmt.Println("unknown command")
	}
}

func stringPtr(s string) *string {
	return &s
}
