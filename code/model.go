package main

import (
	"github.com/lib/pq" // 必须导入，用于处理 Postgres 数组类型
)

const dsn = "host=ep-floral-darkness-a1ubpj9d-pooler.ap-southeast-1.aws.neon.tech user=neondb_owner password=npg_2tgCeacZGv9O dbname=neondb port=5432 sslmode=require TimeZone=Asia/Shanghai"

// AppProfile 模特/服务人员核心信息整合表
type AppProfile struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Lang             string         `gorm:"type:text;not null;index:idx_profile_fast_filter" json:"lang"`    // 展示语言
	Nickname         string         `gorm:"type:text;not null" json:"nickname"`                              // 展示昵称
	PhotoMainURL     string         `gorm:"column:photo_main_url;type:text" json:"photo_main_url"`           // 封面主图URL
	PhotoGalleryURLs pq.StringArray `gorm:"column:photo_gallery_urls;type:text[]" json:"photo_gallery_urls"` // 副图/相册URL数组

	// 分类与区域
	CountryID  int16 `gorm:"column:country_id;not null;default:1;index:idx_profile_fast_filter" json:"country_id"`   // 1:新加坡, 2:马来西亚, 3:泰国
	LocationID int32 `gorm:"column:location_id;not null;index:idx_profile_fast_filter" json:"location_id"`           // 具体区域ID
	CategoryID int16 `gorm:"column:category_id;not null;default:1;index:idx_profile_fast_filter" json:"category_id"` // 业务大类ID
	VenueID    int32 `gorm:"column:venue_id;not null;default:1;index:idx_profile_fast_filter" json:"venue_id"`       // 具体场所ID

	// 运营状态
	BadgeCode int16 `gorm:"column:badge_code;default:0;index:idx_profile_fast_filter" json:"badge_code"` // 角标代码

	// 身体物理规格
	Age      int16  `gorm:"column:age" json:"age"`
	HeightCM int16  `gorm:"column:height_cm" json:"height_cm"`
	WeightKG int16  `gorm:"column:weight_kg" json:"weight_kg"`
	BustSize string `gorm:"column:bust_size;type:text" json:"bust_size"` // 胸围规格 (如: 34D)

	// 服务内容与详细描述
	ServiceItems   string `gorm:"column:service_items;type:text" json:"service_items"`     // 具体服务标签
	BioDescription string `gorm:"column:bio_description;type:text" json:"bio_description"` // 个人简介描述

	// 优先级排序字段 (使用 idx_profile_priority_asc 复合索引)
	LangSort     int16 `gorm:"column:lang_sort;default:0;index:idx_profile_priority_asc" json:"lang_sort"`
	CountrySort  int16 `gorm:"column:country_sort;default:0;index:idx_profile_priority_asc" json:"country_sort"`
	LocationSort int16 `gorm:"column:location_sort;default:0;index:idx_profile_priority_asc" json:"location_sort"`
	CategorySort int16 `gorm:"column:category_sort;default:0;index:idx_profile_priority_asc" json:"category_sort"`
	VenueSort    int16 `gorm:"column:venue_sort;default:0;index:idx_profile_priority_asc" json:"venue_sort"`

	IsActive bool `gorm:"column:is_active;default:true;index:idx_profile_fast_filter" json:"is_active"` // 是否启用
}

// TableName 指定表名
func (AppProfile) TableName() string {
	return "app_profile"
}

// AppContact 客服联系方式表
type AppContact struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ContactType int16  `gorm:"column:contact_type;not null;index:idx_contact_type" json:"contact_type"` // 1:Telegram, 2:WhatsApp
	ContactName string `gorm:"column:contact_name;type:text" json:"contact_name"`                       // 联系人显示名称
	ContactURL  string `gorm:"column:contact_url;type:text;not null" json:"contact_url"`                // 跳转链接
	IsActive    bool   `gorm:"column:is_active;default:true;index:idx_contact_type" json:"is_active"`   // 是否启用
}

// TableName 指定表名
func (AppContact) TableName() string {
	return "app_contact"
}
