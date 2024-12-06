package main

import (
	"fmt"
	"log"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// SimpleMsg 定义了消息的基本结构
type SimpleMsg struct {
	Type      string // 消息类型：private, group 等
	FromID    int64  // 发送者ID
	MessageID int    // 消息ID
	ReplyID   int    // 回复消息ID（如果有）
	Text      string // 消息文本内容
	PhotoID   string // 图片ID（如果有）
	VideoID   string // 视频ID（如果有）
	FileID    string // 文件ID（如果有）
	FileName  string // 文件名称（如果有）
	ChatId    int64  // 聊天ID
	Name      string // 发送者名称
	//SourceForwardId int64
}

// BotHandler 定义了更新事件处理函数类型
type BotHandler func(update tgbotapi.Update)

// emptyLogger 定义了一个空日志记录器
type emptyLogger struct{}

func (l *emptyLogger) Printf(format string, args ...interface{}) {}
func (l *emptyLogger) Println(args ...interface{})               {}

// InitBot 初始化 Telegram 机器人
// mode: polling 或 webhook
// token: Telegram Bot Token
// endpoint: webhook 模式的回调地址
// port: webhook 模式的端口
// handler: 更新事件处理函数
func InitBot(mode, token, endpoint string, port int, handler BotHandler) {
	tgbotapi.SetLogger(&emptyLogger{})
	log.Printf("初始化机器人，模式: %s", mode)

	var err error
	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Printf("创建机器人实例失败: %v", err)
		panic("创建机器人失败: " + err.Error())
	}

	if mode == "webhook" {
		wh, err := tgbotapi.NewWebhook(endpoint)
		if err != nil {
			log.Printf("创建webhook失败: %v", err)
			panic("创建webhook失败: " + err.Error())
		}

		_, err = bot.Request(wh)
		if err != nil {
			log.Printf("设置webhook失败: %v", err)
			panic("设置webhook失败: " + err.Error())
		}

		info, err := bot.GetWebhookInfo()
		if err != nil {
			log.Printf("获取webhook信息失败: %v", err)
			panic("获取webhook信息失败: " + err.Error())
		}

		if info.LastErrorDate != 0 {
			log.Printf("Webhook最后错误: %s", info.LastErrorMessage)
		}

		updates := bot.ListenForWebhook("/")
		go http.ListenAndServe(fmt.Sprintf(":%d", port), nil)

		for update := range updates {
			handler(update)
		}
	} else {
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60

		updates := bot.GetUpdatesChan(u)

		for update := range updates {
			handler(update)
		}
	}
}

// FormatMsg 将 Telegram 更新事件转换为 SimpleMsg 格式
func FormatMsg(update tgbotapi.Update) SimpleMsg {
	msg := SimpleMsg{}
	if update.Message == nil {
		return msg
	}
	if update.Message.Chat != nil {
		msg.Type = update.Message.Chat.Type
		msg.ChatId = update.Message.Chat.ID
	}
	if update.Message.From != nil {
		msg.FromID = update.Message.From.ID
	}
	msg.MessageID = update.Message.MessageID
	msg.Text = update.Message.Text
	msg.Name = fmt.Sprintf("%s %s", update.Message.From.FirstName, update.Message.From.LastName)
	if update.Message.ReplyToMessage != nil {
		msg.ReplyID = update.Message.ReplyToMessage.MessageID
	}
	if update.Message.Photo != nil {
		if len(update.Message.Photo) > 0 {
			msg.PhotoID = update.Message.Photo[0].FileID
		}
	}
	if update.Message.Video != nil {
		msg.VideoID = update.Message.Video.FileID
	}

	if update.Message.Document != nil {
		msg.FileID = update.Message.Document.FileID
		msg.FileName = update.Message.Document.FileName
	}
	return msg
}

// SendMsg 发送文本消息
func SendMsg(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg)
}

// ReplyMsg 回复文本消息
func ReplyMsg(chatID int64, text string, replyTo int) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyTo
	bot.Send(msg)
}

// SendExistingPhoto 转发已存在的图片
func SendExistingPhoto(chatID int64, photoID string) {
	msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(photoID))
	bot.Send(msg)
}

// SendExistingVideo 转发已存在的视频
func SendExistingVideo(chatID int64, videoID string) {
	msg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(videoID))
	bot.Send(msg)
}

// SendExistingFile 转发已存在的文件
func SendExistingFile(chatID int64, fileID string, fileName string) {
	msg := tgbotapi.NewDocument(chatID, tgbotapi.FileID(fileID))
	msg.Caption = fileName
	bot.Send(msg)
}

// ForwardMsg 转发消息
func ForwardMsg(chatID int64, fromChatID int64, messageID int) int {
	msg := tgbotapi.NewForward(chatID, fromChatID, messageID)
	returinfo, _ := bot.Send(msg)
	return returinfo.MessageID
}
