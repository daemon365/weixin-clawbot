package weixin

type BaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
}

const (
	UploadMediaTypeImage = 1
	UploadMediaTypeVideo = 2
	UploadMediaTypeFile  = 3
	UploadMediaTypeVoice = 4
)

type GetUploadURLRequest struct {
	FileKey         string `json:"filekey,omitempty"`
	MediaType       int    `json:"media_type,omitempty"`
	ToUserID        string `json:"to_user_id,omitempty"`
	RawSize         int64  `json:"rawsize,omitempty"`
	RawFileMD5      string `json:"rawfilemd5,omitempty"`
	FileSize        int64  `json:"filesize,omitempty"`
	ThumbRawSize    int64  `json:"thumb_rawsize,omitempty"`
	ThumbRawFileMD5 string `json:"thumb_rawfilemd5,omitempty"`
	ThumbFileSize   int64  `json:"thumb_filesize,omitempty"`
	NoNeedThumb     bool   `json:"no_need_thumb,omitempty"`
	AESKey          string `json:"aeskey,omitempty"`
}

type GetUploadURLResponse struct {
	UploadParam      string `json:"upload_param,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
}

const (
	MessageTypeNone = 0
	MessageTypeUser = 1
	MessageTypeBot  = 2
)

const (
	MessageItemTypeNone  = 0
	MessageItemTypeText  = 1
	MessageItemTypeImage = 2
	MessageItemTypeVoice = 3
	MessageItemTypeFile  = 4
	MessageItemTypeVideo = 5
)

const (
	MessageStateNew        = 0
	MessageStateGenerating = 1
	MessageStateFinish     = 2
)

type TextItem struct {
	Text string `json:"text,omitempty"`
}

type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
}

type ImageItem struct {
	Media       *CDNMedia `json:"media,omitempty"`
	ThumbMedia  *CDNMedia `json:"thumb_media,omitempty"`
	AESKeyHex   string    `json:"aeskey,omitempty"`
	URL         string    `json:"url,omitempty"`
	MidSize     int64     `json:"mid_size,omitempty"`
	ThumbSize   int64     `json:"thumb_size,omitempty"`
	ThumbHeight int64     `json:"thumb_height,omitempty"`
	ThumbWidth  int64     `json:"thumb_width,omitempty"`
	HDSize      int64     `json:"hd_size,omitempty"`
}

type VoiceItem struct {
	Media         *CDNMedia `json:"media,omitempty"`
	EncodeType    int       `json:"encode_type,omitempty"`
	BitsPerSample int       `json:"bits_per_sample,omitempty"`
	SampleRate    int       `json:"sample_rate,omitempty"`
	Playtime      int64     `json:"playtime,omitempty"`
	Text          string    `json:"text,omitempty"`
}

type FileItem struct {
	Media    *CDNMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	MD5      string    `json:"md5,omitempty"`
	Length   string    `json:"len,omitempty"`
}

type VideoItem struct {
	Media       *CDNMedia `json:"media,omitempty"`
	VideoSize   int64     `json:"video_size,omitempty"`
	PlayLength  int64     `json:"play_length,omitempty"`
	VideoMD5    string    `json:"video_md5,omitempty"`
	ThumbMedia  *CDNMedia `json:"thumb_media,omitempty"`
	ThumbSize   int64     `json:"thumb_size,omitempty"`
	ThumbHeight int64     `json:"thumb_height,omitempty"`
	ThumbWidth  int64     `json:"thumb_width,omitempty"`
}

type RefMessage struct {
	MessageItem *MessageItem `json:"message_item,omitempty"`
	Title       string       `json:"title,omitempty"`
}

type MessageItem struct {
	Type         int         `json:"type,omitempty"`
	CreateTimeMS int64       `json:"create_time_ms,omitempty"`
	UpdateTimeMS int64       `json:"update_time_ms,omitempty"`
	IsCompleted  bool        `json:"is_completed,omitempty"`
	MsgID        string      `json:"msg_id,omitempty"`
	RefMessage   *RefMessage `json:"ref_msg,omitempty"`
	TextItem     *TextItem   `json:"text_item,omitempty"`
	ImageItem    *ImageItem  `json:"image_item,omitempty"`
	VoiceItem    *VoiceItem  `json:"voice_item,omitempty"`
	FileItem     *FileItem   `json:"file_item,omitempty"`
	VideoItem    *VideoItem  `json:"video_item,omitempty"`
}

type WeixinMessage struct {
	Seq          int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMS int64         `json:"create_time_ms,omitempty"`
	UpdateTimeMS int64         `json:"update_time_ms,omitempty"`
	DeleteTimeMS int64         `json:"delete_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []MessageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type GetUpdatesRequest struct {
	SyncBuf       string `json:"sync_buf,omitempty"`
	GetUpdatesBuf string `json:"get_updates_buf,omitempty"`
}

type GetUpdatesResponse struct {
	Ret                  int             `json:"ret,omitempty"`
	ErrCode              int             `json:"errcode,omitempty"`
	ErrMsg               string          `json:"errmsg,omitempty"`
	Messages             []WeixinMessage `json:"msgs,omitempty"`
	SyncBuf              string          `json:"sync_buf,omitempty"`
	GetUpdatesBuf        string          `json:"get_updates_buf,omitempty"`
	LongPollingTimeoutMS int             `json:"longpolling_timeout_ms,omitempty"`
}

type SendMessageRequest struct {
	Message *WeixinMessage `json:"msg,omitempty"`
}

const (
	TypingStatusTyping = 1
	TypingStatusCancel = 2
)

type SendTypingRequest struct {
	ILinkUserID  string `json:"ilink_user_id,omitempty"`
	TypingTicket string `json:"typing_ticket,omitempty"`
	Status       int    `json:"status,omitempty"`
}

type SendTypingResponse struct {
	Ret    int    `json:"ret,omitempty"`
	ErrMsg string `json:"errmsg,omitempty"`
}

type GetConfigResponse struct {
	Ret          int    `json:"ret,omitempty"`
	ErrMsg       string `json:"errmsg,omitempty"`
	TypingTicket string `json:"typing_ticket,omitempty"`
}
