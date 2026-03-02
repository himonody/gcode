package main

import (
	"fmt"
	"github.com/lib/pq"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"log"
	"strconv"
	"strings"
)

func main() {
	// 1. 配置 DSN (Data Source Name)
	// host: 数据库地址, user: 用户名, password: 密码, dbname: 数据库名, port: 端口, sslmode: 是否启用SSL

	InitPg()
	fmt.Println("数据库连接成功！")
	if err := ImportAppProfileExcel(DB, "./a.excel"); err != nil {
		fmt.Println(err)
	}
	fmt.Println("数据写入成功！")

}

// ImportAppProfileExcel 执行 Excel 到数据库的完整导入
func ImportAppProfileExcel(db *gorm.DB, filePath string) error {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return fmt.Errorf("无法打开Excel: %w", err)
	}
	defer f.Close()

	// 获取第一个 Sheet 的所有行
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		return fmt.Errorf("读取Sheet失败: %w", err)
	}

	var profiles []AppProfile

	// 遍历行 (假设第一行是标题头)
	for i, row := range rows {
		if i == 0 || len(row) == 0 {
			continue
		}

		// 辅助函数：安全获取 Excel 列数据，防止数组越界
		getCol := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		// 处理数字转换的辅助函数
		toInt16 := func(s string) int16 {
			v, _ := strconv.ParseInt(s, 10, 16)
			return int16(v)
		}
		toInt32 := func(s string) int32 {
			v, _ := strconv.ParseInt(s, 10, 32)
			return int32(v)
		}

		// 构造模型（手动映射 Excel 列到字段）
		profile := AppProfile{
			// 1. 基础信息
			Lang:         getCol(0), // 第A列
			Nickname:     getCol(1), // 第B列
			PhotoMainURL: getCol(2), // 第C列
			// 相册URL数组 (假设Excel中用英文逗号或换行分隔)
			PhotoGalleryURLs: pq.StringArray(parseArray(getCol(3))),

			// 2. 分类与区域
			CountryID:  toInt16(getCol(4)),
			LocationID: toInt32(getCol(5)),
			CategoryID: toInt16(getCol(6)),
			VenueID:    toInt32(getCol(7)),

			// 3. 运营状态
			BadgeCode: toInt16(getCol(8)),

			// 4. 身体规格
			Age:      toInt16(getCol(9)),
			HeightCM: toInt16(getCol(10)),
			WeightKG: toInt16(getCol(11)),
			BustSize: getCol(12),

			// 5. 服务与描述
			ServiceItems:   getCol(13),
			BioDescription: getCol(14),

			// 6. 排序优先级
			LangSort:     toInt16(getCol(15)),
			CountrySort:  toInt16(getCol(16)),
			LocationSort: toInt16(getCol(17)),
			CategorySort: toInt16(getCol(18)),
			VenueSort:    toInt16(getCol(19)),

			// 7. 状态
			IsActive: getCol(20) != "false", // 默认为 true，除非填了 false
		}

		profiles = append(profiles, profile)
	}

	// 使用事务批量插入，每 100 条为一个批次执行一次 SQL
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&profiles, 100).Error; err != nil {
			return err
		}
		log.Printf("成功导入 %d 条模特数据", len(profiles))
		return nil
	})
}

// 辅助函数：将字符串按逗号切分为切片
func parseArray(s string) []string {
	if s == "" {
		return []string{}
	}
	// 处理常见的几种分隔符：逗号、分号、换行
	s = strings.ReplaceAll(s, "，", ",")
	s = strings.ReplaceAll(s, "\n", ",")
	parts := strings.Split(s, ",")

	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}
