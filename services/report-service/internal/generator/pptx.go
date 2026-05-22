package generator

import (
	"fmt"
	"strings"
)

// OOXML namespace attributes shared by every presentation part.
const pptxNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

// renderPPTX emits a minimal OOXML presentation (.pptx): a title slide
// followed by one slide per report section. Tables are rendered as
// pipe-joined text lines.
func renderPPTX(doc Document) ([]byte, error) {
	type slide struct {
		title string
		body  []string
	}
	var slides []slide

	titleBody := []string{}
	if doc.Subtitle != "" {
		titleBody = append(titleBody, doc.Subtitle)
	}
	titleBody = append(titleBody, "Generated "+doc.GeneratedAt)
	if doc.Engine != "" {
		titleBody = append(titleBody, "Engine: "+doc.Engine)
	}
	for _, h := range doc.Highlights {
		titleBody = append(titleBody, h.Label+": "+h.Value)
	}
	title := doc.Title
	if title == "" {
		title = "Report"
	}
	slides = append(slides, slide{title, titleBody})

	for _, s := range doc.Sections {
		var body []string
		if s.Summary != "" {
			body = append(body, s.Summary)
		}
		tbl := tabulate(s.Rows)
		if len(tbl.Columns) > 0 {
			body = append(body, strings.Join(tbl.Columns, "  |  "))
			for _, r := range tbl.Rows {
				body = append(body, strings.Join(r, "  |  "))
			}
		}
		slides = append(slides, slide{s.Title, body})
	}

	var sldIds, presRels, ctOverrides strings.Builder
	slideEntries := make([]zipEntry, 0, 2*len(slides))
	for i, s := range slides {
		n := i + 1
		rid := i + 2 // rId1 is reserved for the slide master
		slideEntries = append(
			slideEntries,
			zipEntry{fmt.Sprintf("ppt/slides/slide%d.xml", n), []byte(pptxSlideXML(s.title, s.body))},
			zipEntry{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), []byte(pptxSlideRels)},
		)
		fmt.Fprintf(&sldIds, `<p:sldId id="%d" r:id="rId%d"/>`, 255+n, rid)
		fmt.Fprintf(&presRels,
			`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`,
			rid, n)
		fmt.Fprintf(&ctOverrides,
			`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`,
			n)
	}

	contentTypes := xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
		`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
		`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
		`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
		ctOverrides.String() + `</Types>`

	presentation := xmlDecl + `<p:presentation ` + pptxNS + `>` +
		`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
		`<p:sldIdLst>` + sldIds.String() + `</p:sldIdLst>` +
		`<p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/>` +
		`</p:presentation>`

	presentationRels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>` +
		presRels.String() + `</Relationships>`

	entries := []zipEntry{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(pptxRootRels)},
		{"ppt/presentation.xml", []byte(presentation)},
		{"ppt/_rels/presentation.xml.rels", []byte(presentationRels)},
		{"ppt/slideMasters/slideMaster1.xml", []byte(pptxSlideMaster)},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", []byte(pptxMasterRels)},
		{"ppt/slideLayouts/slideLayout1.xml", []byte(pptxSlideLayout)},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", []byte(pptxLayoutRels)},
		{"ppt/theme/theme1.xml", []byte(pptxTheme)},
	}
	entries = append(entries, slideEntries...)
	return buildZip(entries)
}

// pptxSlideXML renders one slide: a title text box and a body text box
// with one paragraph per body line.
func pptxSlideXML(title string, body []string) string {
	var paras strings.Builder
	if len(body) == 0 {
		paras.WriteString(`<a:p/>`)
	}
	for _, line := range body {
		fmt.Fprintf(&paras,
			`<a:p><a:r><a:rPr lang="en-US" sz="1600" dirty="0"/><a:t>%s</a:t></a:r></a:p>`,
			xmlEscape(line))
	}
	return xmlDecl + `<p:sld ` + pptxNS + `><p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
		`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="685800" y="457200"/><a:ext cx="10820400" cy="1143000"/></a:xfrm>` +
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` +
		`<a:p><a:r><a:rPr lang="en-US" sz="3200" b="1" dirty="0"/><a:t>` + xmlEscape(title) +
		`</a:t></a:r></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Body"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="685800" y="1828800"/><a:ext cx="10820400" cy="4572000"/></a:xfrm>` +
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` + paras.String() + `</p:txBody></p:sp>` +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

const pptxRootRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
	`</Relationships>`

const pptxSlideRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
	`</Relationships>`

const pptxMasterRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>` +
	`</Relationships>`

const pptxLayoutRels = xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>` +
	`</Relationships>`

const pptxSlideMaster = xmlDecl + `<p:sldMaster ` + pptxNS + `><p:cSld>` +
	`<p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg>` +
	`<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree></p:cSld>` +
	`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" ` +
	`accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
	`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`

const pptxSlideLayout = xmlDecl + `<p:sldLayout ` + pptxNS + ` type="blank" preserve="1">` +
	`<p:cSld name="Blank"><p:spTree>` +
	`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree></p:cSld>` +
	`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

const pptxTheme = xmlDecl + `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="OpenFoundry">` +
	`<a:themeElements><a:clrScheme name="OpenFoundry">` +
	`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2>` +
	`<a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>` +
	`<a:fontScheme name="OpenFoundry">` +
	`<a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
	`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="OpenFoundry">` +
	`<a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst>` +
	`<a:ln w="6350" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>` +
	`<a:ln w="12700" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>` +
	`<a:ln w="19050" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>` +
	`</a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst>` +
	`</a:fmtScheme></a:themeElements></a:theme>`
