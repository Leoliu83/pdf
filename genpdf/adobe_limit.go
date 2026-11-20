package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type pdfBuilder struct {
	w       *bufio.Writer
	offsets map[int]int64
	pos     int64
	maxID   int
}

func newPDFBuilder(f *os.File) *pdfBuilder {
	return &pdfBuilder{
		w:       bufio.NewWriterSize(f, 1<<20), // 1MB 写缓冲，生成百万页极快
		offsets: make(map[int]int64),
	}
}

func (b *pdfBuilder) write(s string) {
	n, _ := b.w.WriteString(s)
	b.pos += int64(n)
}

func (b *pdfBuilder) writeObj(id int, body string) {
	b.offsets[id] = b.pos
	if id > b.maxID {
		b.maxID = id
	}
	b.write(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", id, body))
}

func (b *pdfBuilder) finish() error {
	xrefPos := b.pos
	b.write(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", b.maxID+1))
	for i := 1; i <= b.maxID; i++ {
		if off, ok := b.offsets[i]; ok {
			b.write(fmt.Sprintf("%010d 00000 n \n", off))
		} else {
			b.write("0000000000 65535 f \n")
		}
	}
	b.write(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", b.maxID+1, xrefPos))
	return b.w.Flush()
}

// 1. 生成多层嵌套、多分支的测试 PDF（共 5 页，3 层嵌套）
func genNestedPDF(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	b := newPDFBuilder(f)
	b.write("%PDF-1.4\n")
	b.writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.writeObj(12, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	b.writeObj(13, "<< /Font << /F1 12 0 R >> >>")

	// 根 Pages -> [Page1, BranchA, BranchB]
	b.writeObj(2, "<< /Type /Pages /Kids [ 3 0 R 4 0 R 11 0 R ] /Count 5 >>")
	// 分支 A -> [Page2, SubBranchA1]
	b.writeObj(4, "<< /Type /Pages /Parent 2 0 R /Kids [ 5 0 R 6 0 R ] /Count 3 >>")
	// 子分支 A1 -> [Page3, Page4]
	b.writeObj(6, "<< /Type /Pages /Parent 4 0 R /Kids [ 7 0 R 8 0 R ] /Count 2 >>")
	// 分支 B -> [Page5]
	b.writeObj(11, "<< /Type /Pages /Parent 2 0 R /Kids [ 10 0 R ] /Count 1 >>")

	pages := []struct {
		pageID, parentID, streamID int
		text                       string
	}{
		{3, 2, 14, "Page 1 - Level 1"},
		{5, 4, 15, "Page 2 - Level 2 (Branch A)"},
		{7, 6, 16, "Page 3 - Level 3 (SubBranch A1)"},
		{8, 6, 17, "Page 4 - Level 3 (SubBranch A1)"},
		{10, 11, 18, "Page 5 - Level 2 (Branch B)"},
	}

	for _, p := range pages {
		stream := fmt.Sprintf("BT /F1 16 Tf 72 720 Td (%s) Tj ET", p.text)
		b.writeObj(p.streamID, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
		b.writeObj(p.pageID, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources 13 0 R /Contents %d 0 R >>", p.parentID, p.streamID))
	}

	return b.finish()
}

// 2. 生成 /Count 不准确的测试 PDF（实际 3 页，根节点写 9999999，子分支写 0）
func genInaccurateCountPDF(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	b := newPDFBuilder(f)
	b.write("%PDF-1.4\n")
	b.writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.writeObj(7, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	b.writeObj(8, "<< /Font << /F1 7 0 R >> >>")

	// 故意把根节点 /Count 设为极大值 9999999，子分支设为 0
	b.writeObj(2, "<< /Type /Pages /Kids [ 3 0 R 4 0 R ] /Count 9999999 >>")
	b.writeObj(4, "<< /Type /Pages /Parent 2 0 R /Kids [ 5 0 R 6 0 R ] /Count 0 >>")

	pages := []struct {
		pageID, parentID, streamID int
		text                       string
	}{
		{3, 2, 9, "Actual Page 1"},
		{5, 4, 10, "Actual Page 2"},
		{6, 4, 11, "Actual Page 3"},
	}

	for _, p := range pages {
		stream := fmt.Sprintf("BT /F1 16 Tf 72 720 Td (%s) Tj ET", p.text)
		b.writeObj(p.streamID, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
		b.writeObj(p.pageID, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources 8 0 R /Contents %d 0 R >>", p.parentID, p.streamID))
	}

	return b.finish()
}

// 3. 生成极限大页数压测 PDF（totalPages 设为 8_380_000 即逼近 Adobe 838 万页上限）
func genHugePDF(filename string, totalPages int) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	b := newPDFBuilder(f)
	b.write("%PDF-1.4\n")
	b.writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	b.writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	b.writeObj(4, "<< /Font << /F1 3 0 R >> >>")
	stream := "BT /F1 18 Tf 72 720 Td (Limit Test Page) Tj ET"
	b.writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))

	// 每 100 页挂载在一个中间 /Pages 节点下，避免根数组单行过长
	const batchSize = 100
	firstPageID := 6
	numGroups := (totalPages + batchSize - 1) / batchSize
	firstGroupID := firstPageID + totalPages

	var rootKids strings.Builder
	for g := 0; g < numGroups; g++ {
		groupID := firstGroupID + g
		if g > 0 {
			rootKids.WriteByte(' ')
		}
		rootKids.WriteString(strconv.Itoa(groupID))
		rootKids.WriteString(" 0 R")

		startPage := g * batchSize
		endPage := startPage + batchSize
		if endPage > totalPages {
			endPage = totalPages
		}

		var groupKids strings.Builder
		for p := startPage; p < endPage; p++ {
			pid := firstPageID + p
			if p > startPage {
				groupKids.WriteByte(' ')
			}
			groupKids.WriteString(strconv.Itoa(pid))
			groupKids.WriteString(" 0 R")

			b.writeObj(pid, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources 4 0 R /Contents 5 0 R >>", groupID))
		}
		b.writeObj(groupID, fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Kids [ %s ] /Count %d >>", groupKids.String(), endPage-startPage))
	}

	b.writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [ %s ] /Count %d >>", rootKids.String(), totalPages))
	return b.finish()
}

func main() {
	_ = genNestedPDF("nested_pages_test.pdf")
	_ = genInaccurateCountPDF("inaccurate_count_test.pdf")

	start := time.Now()
	// 默认生成 100 万页（约 130MB，2秒内完成）；想测 Adobe 极限可直接改为 8_380_000
	if err := genHugePDF("adobe_limit_test.pdf", 1_000_000); err != nil {
		panic(err)
	}
	fmt.Printf("全部测试 PDF 生成完成，耗时: %v\n", time.Since(start))
}
