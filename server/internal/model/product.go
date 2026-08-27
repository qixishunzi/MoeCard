package model

import "time"

// Category 是商品分类。
type Category struct {
	Model
	Name        string `gorm:"column:name;size:64" json:"name"`
	Slug        string `gorm:"column:slug;size:100;uniqueIndex" json:"slug"`
	Description string `gorm:"column:description;size:500" json:"description"`
	Icon        string `gorm:"column:icon;size:255" json:"icon"`
	Sort        int    `gorm:"column:sort" json:"sort"`
	Status      string `gorm:"column:status;size:16" json:"status"`

	// 非数据库字段：列表接口带出的商品数
	ProductCount int64 `gorm:"-" json:"product_count,omitempty"`
}

func (Category) TableName() string { return "categories" }

// 发货类型。
const (
	DeliveryAuto   = "auto"   // 自动发货：从 product_codes 取卡密
	DeliveryManual = "manual" // 手动发货：管理员后台填写内容
)

// 商品上下架状态。
const (
	ProductStatusOn  = "on"
	ProductStatusOff = "off"
)

// StockUnlimited 表示无限库存（仅对 manual 商品有效）。
const StockUnlimited int64 = -1

// Product 是商品。
//
// 库存语义：
//   - delivery_type = auto   → 真实可用库存 = product_codes 中 status='unused' 的数量，
//     Stock 字段不参与计算（保持 0）。
//   - delivery_type = manual → 使用 Stock 字段；-1 表示无限。
type Product struct {
	Model
	CategoryID    uint64     `gorm:"column:category_id;index" json:"category_id"`
	Name          string     `gorm:"column:name;size:191" json:"name"`
	Slug          string     `gorm:"column:slug;size:150;uniqueIndex" json:"slug"`
	Cover         string     `gorm:"column:cover;size:500" json:"cover"`
	Summary       string     `gorm:"column:summary;size:500" json:"summary"`
	Description   string     `gorm:"column:description" json:"description"`
	Price         int64      `gorm:"column:price" json:"price"`
	OriginalPrice int64      `gorm:"column:original_price" json:"original_price"`
	Stock         int64      `gorm:"column:stock" json:"stock"`
	DeliveryType  string     `gorm:"column:delivery_type;size:16" json:"delivery_type"`
	Status        string     `gorm:"column:status;size:16" json:"status"`
	Sort          int        `gorm:"column:sort" json:"sort"`
	SalesCount    int64      `gorm:"column:sales_count" json:"sales_count"`
	IsRecommend   bool       `gorm:"column:is_recommend" json:"is_recommend"`
	MinQuantity   int        `gorm:"column:min_quantity" json:"min_quantity"`
	MaxQuantity   int        `gorm:"column:max_quantity" json:"max_quantity"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;index" json:"-"`

	// LowStockThreshold 为 0 时沿用全局设置 low_stock_threshold。
	LowStockThreshold int `gorm:"column:low_stock_threshold" json:"low_stock_threshold"`
	// LowStockNotifiedAt 抑制重复告警：补货回升到阈值以上时清空，再次跌破才会重新提醒。
	LowStockNotifiedAt *time.Time `gorm:"column:low_stock_notified_at" json:"-"`
	// CustomFields 是买家下单时需额外填写的字段定义（JSON 数组）。
	CustomFields string `gorm:"column:custom_fields" json:"-"`

	// 非数据库字段
	CategoryName   string `gorm:"-" json:"category_name,omitempty"`
	AvailableStock int64  `gorm:"-" json:"available_stock"`
	// CustomFieldList 是 CustomFields 解析后的结构，出参用这个而不是原始字符串。
	CustomFieldList []CustomField `gorm:"-" json:"custom_fields"`
}

// CustomField 描述下单页要额外收集的一项信息。
//
// 代充、账号租赁这类手动发货商品必须知道买家的游戏账号 / UID，
// 否则商家收了钱也不知道该充给谁。
type CustomField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | textarea | select
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"` // type=select 时的可选值
	MaxLen      int      `json:"max_len,omitempty"` // 0 表示用默认上限
	Pattern     string   `json:"pattern,omitempty"` // 可选的正则校验
}

func (Product) TableName() string { return "products" }

// IsOnSale 判断商品是否可售。
func (p *Product) IsOnSale() bool { return p.Status == ProductStatusOn && p.DeletedAt == nil }

// IsAuto 判断是否自动发货。
func (p *Product) IsAuto() bool { return p.DeliveryType == DeliveryAuto }

// IsUnlimitedStock 判断手动发货商品是否无限库存。
func (p *Product) IsUnlimitedStock() bool {
	return p.DeliveryType == DeliveryManual && p.Stock == StockUnlimited
}

// 卡密状态。流转方向只允许 unused → locked → sold，绝不回退到 sold 之前。
const (
	CodeStatusUnused = "unused"
	CodeStatusLocked = "locked"
	CodeStatusSold   = "sold"
)

// ProductCode 是卡密。
type ProductCode struct {
	ID        uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ProductID uint64 `gorm:"column:product_id;index" json:"product_id"`
	// Content 是落库形态：启用静态加密后为 "enc:v1:<base64>"，
	// 未启用或加密前写入的历史数据仍是明文。取用一律走 utils.Decrypt。
	Content     string     `gorm:"column:content;size:2000" json:"content"`
	ContentHash string     `gorm:"column:content_hash;size:64" json:"-"`
	Encrypted   bool       `gorm:"column:encrypted" json:"-"`
	Status      string     `gorm:"column:status;size:16" json:"status"`
	OrderID     uint64     `gorm:"column:order_id" json:"order_id"`
	OrderItemID uint64     `gorm:"column:order_item_id" json:"order_item_id"`
	LockedAt    *time.Time `gorm:"column:locked_at" json:"locked_at"`
	SoldAt      *time.Time `gorm:"column:sold_at" json:"sold_at"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"created_at"`

	// 非数据库字段
	OrderNo string `gorm:"-" json:"order_no,omitempty"`
}

func (ProductCode) TableName() string { return "product_codes" }
