package export

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/phpdave11/gofpdf"
	"packaging-ai/internal/domain"
)

//go:embed fonts/NotoSerifSC.ttf
var chineseFont []byte

type ReportData struct {
	Project   domain.Project
	Brief     domain.Brief
	Proposals []domain.DesignProposal
}

func Markdown(data ReportData) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", data.Project.Name)
	fmt.Fprintf(&b, "- 项目 ID：%s\n- 当前状态：%s\n- 当前轮次：%d\n\n", data.Project.ID, data.Project.Status, data.Project.CurrentRound)
	b.WriteString("## 设计 Brief\n\n")
	fmt.Fprintf(&b, "- 目标客户：%s\n- 市场：%s\n- 材质：%s\n- 印刷工艺：%s\n- 完整度：%d%%\n\n", data.Brief.Audience, data.Brief.Market, data.Brief.Material, data.Brief.PrintingProcess, data.Brief.CompletionScore)
	fmt.Fprintf(&b, "- 产品信息：`%v`\n- 包装规格：`%v`\n- 必填文字：%s\n\n", data.Brief.ProductInfo, data.Brief.PackageSpec, strings.Join(data.Brief.RequiredText, "、"))
	b.WriteString("## 设计方案\n\n")
	for i, proposal := range data.Proposals {
		fmt.Fprintf(&b, "### %d. %s\n\n", i+1, proposal.Title)
		fmt.Fprintf(&b, "- 轮次：%d\n- 创意：%s\n- 设计策略：`%v`\n- 配色：%s\n- 字体：%s\n- 差异说明：%s\n", proposal.Round, proposal.Concept, proposal.DesignStrategy, strings.Join(proposal.ColorPalette, "、"), proposal.Typography, proposal.Difference)
		for _, asset := range proposal.Assets {
			fmt.Fprintf(&b, "- 图片资产：`%s`\n", asset.ID)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func PDF(data ReportData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("NotoSC", "", chineseFont)
	pdf.SetTitle(data.Project.Name+" Packaging Report", true)
	pdf.SetAuthor("Packaging AI", true)
	pdf.AddPage()
	pdf.SetFont("NotoSC", "", 20)
	pdf.CellFormat(0, 12, data.Project.Name, "", 1, "L", false, 0, "")
	pdf.SetFont("NotoSC", "", 10)
	pdf.SetTextColor(90, 100, 95)
	pdf.CellFormat(0, 7, fmt.Sprintf("Packaging AI | 项目 %s | 第 %d 轮", data.Project.ID, data.Project.CurrentRound), "", 1, "L", false, 0, "")
	pdf.Ln(8)
	section := func(title string) {
		pdf.SetTextColor(24, 59, 52)
		pdf.SetFont("NotoSC", "", 14)
		pdf.CellFormat(0, 9, title, "", 1, "L", false, 0, "")
		pdf.SetTextColor(35, 42, 40)
		pdf.SetFont("NotoSC", "", 10)
	}
	section("设计 Brief")
	pdf.MultiCell(0, 6, fmt.Sprintf("目标客户：%s\n市场：%s\n材质：%s\n印刷工艺：%s\n完整度：%d%%\n产品信息：%v\n包装规格：%v\n必填文字：%s", data.Brief.Audience, data.Brief.Market, data.Brief.Material, data.Brief.PrintingProcess, data.Brief.CompletionScore, data.Brief.ProductInfo, data.Brief.PackageSpec, strings.Join(data.Brief.RequiredText, "、")), "", "L", false)
	pdf.Ln(7)
	section("设计方案")
	for i, proposal := range data.Proposals {
		pdf.SetFont("NotoSC", "", 11)
		pdf.CellFormat(0, 7, fmt.Sprintf("%d. %s", i+1, proposal.Title), "", 1, "L", false, 0, "")
		pdf.SetFont("NotoSC", "", 10)
		pdf.MultiCell(0, 5.5, fmt.Sprintf("创意：%s\n设计策略：%v\n配色：%s\n字体：%s\n差异说明：%s\n图片资产：%d", proposal.Concept, proposal.DesignStrategy, strings.Join(proposal.ColorPalette, "、"), proposal.Typography, proposal.Difference, len(proposal.Assets)), "", "L", false)
		for assetIndex, asset := range proposal.Assets {
			imageData, imageType, err := loadImage(context.Background(), asset.StorageURL)
			if err != nil || imageType == "" {
				continue
			}
			name := fmt.Sprintf("proposal-%d-%d", i, assetIndex)
			options := gofpdf.ImageOptions{ImageType: imageType, ReadDpi: true}
			pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(imageData))
			pdf.ImageOptions(name, 20, pdf.GetY()+3, 72, 0, false, options, 0, "")
			pdf.SetY(pdf.GetY() + 58)
		}
		pdf.Ln(4)
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func loadImage(ctx context.Context, source string) ([]byte, string, error) {
	if strings.HasPrefix(source, "data:") {
		parts := strings.SplitN(source, ",", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid data URL")
		}
		header, payload := parts[0], parts[1]
		imageType := strings.ToUpper(strings.TrimPrefix(strings.Split(header, ";")[0], "data:image/"))
		if strings.Contains(header, ";base64") {
			data, err := base64.StdEncoding.DecodeString(payload)
			return data, imageType, err
		}
		data, err := url.PathUnescape(payload)
		return []byte(data), imageType, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("image status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	imageType := strings.ToUpper(strings.TrimPrefix(response.Header.Get("Content-Type"), "image/"))
	if imageType == "" {
		imageType = "PNG"
	}
	return data, imageType, nil
}
