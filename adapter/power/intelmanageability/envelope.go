package intelmanageability

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
)

// WS-Management/WS-Addressing/WS-Enumerationの標準namespace。ローカルなfmt.Sprintfによる文字列組み立てではなく、encoding/xmlのEncoderでnamespace付きelementを組み立てることでescapingと構造を保証する。
const (
	nsSOAPEnvelope = "http://www.w3.org/2003/05/soap-envelope"
	nsAddressing   = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	nsWSManagement = "http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"
	nsWSTransfer   = "http://schemas.xmlsoap.org/ws/2004/09/transfer"
	nsWSEnumerate  = "http://schemas.xmlsoap.org/ws/2004/09/enumeration"
	nsAnonymous    = "http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous"

	actionTransferGet = nsWSTransfer + "/Get"
	actionEnumerate   = nsWSEnumerate + "/Enumerate"
	actionPull        = nsWSEnumerate + "/Pull"
)

// invokeParamはWS-Man method invocationの入力parameterである。referenceが設定されている場合はEndpointReferenceとしてencodeし、そうでなければ単純なtext要素としてencodeする。
type invokeParam struct {
	name      string
	value     string
	reference *endpointReference
}

// endpointReferenceはWS-Addressingのreference(EPR)である。RequestPowerStateChangeのManagedElementのように、他のCIM instanceを参照するparameterに使う。
type endpointReference struct {
	address     string
	resourceURI string
	selectors   []invokeParam
}

func newMessageID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate WS-Man message ID: %w", err)
	}
	return "uuid:" + hex.EncodeToString(buf), nil
}

// buildEnvelopeはWS-Man SOAP requestを組み立てる。bodyWriterがnilの場合、Bodyは空要素(WS-Transfer Get相当)になる。
func buildEnvelope(to, resourceURI, action, messageID string, selectors []invokeParam, bodyWriter func(*xml.Encoder) error) ([]byte, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)

	envelopeStart := xml.StartElement{Name: xml.Name{Space: nsSOAPEnvelope, Local: "Envelope"}}
	if err := enc.EncodeToken(envelopeStart); err != nil {
		return nil, err
	}

	if err := writeHeader(enc, to, resourceURI, action, messageID, selectors); err != nil {
		return nil, err
	}

	bodyStart := xml.StartElement{Name: xml.Name{Space: nsSOAPEnvelope, Local: "Body"}}
	if err := enc.EncodeToken(bodyStart); err != nil {
		return nil, err
	}
	if bodyWriter != nil {
		if err := bodyWriter(enc); err != nil {
			return nil, err
		}
	}
	if err := enc.EncodeToken(bodyStart.End()); err != nil {
		return nil, err
	}

	if err := enc.EncodeToken(envelopeStart.End()); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeHeader(enc *xml.Encoder, to, resourceURI, action, messageID string, selectors []invokeParam) error {
	headerStart := xml.StartElement{Name: xml.Name{Space: nsSOAPEnvelope, Local: "Header"}}
	if err := enc.EncodeToken(headerStart); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsAddressing, "To", to); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsWSManagement, "ResourceURI", resourceURI); err != nil {
		return err
	}

	replyToStart := xml.StartElement{Name: xml.Name{Space: nsAddressing, Local: "ReplyTo"}}
	if err := enc.EncodeToken(replyToStart); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsAddressing, "Address", nsAnonymous); err != nil {
		return err
	}
	if err := enc.EncodeToken(replyToStart.End()); err != nil {
		return err
	}

	if err := writeTextElement(enc, nsAddressing, "Action", action); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsAddressing, "MessageID", messageID); err != nil {
		return err
	}

	if len(selectors) > 0 {
		if err := writeSelectorSet(enc, selectors); err != nil {
			return err
		}
	}

	return enc.EncodeToken(headerStart.End())
}

func writeSelectorSet(enc *xml.Encoder, selectors []invokeParam) error {
	selectorSetStart := xml.StartElement{Name: xml.Name{Space: nsWSManagement, Local: "SelectorSet"}}
	if err := enc.EncodeToken(selectorSetStart); err != nil {
		return err
	}
	for _, selector := range selectors {
		selectorStart := xml.StartElement{
			Name: xml.Name{Space: nsWSManagement, Local: "Selector"},
			Attr: []xml.Attr{{Name: xml.Name{Local: "Name"}, Value: selector.name}},
		}
		if err := enc.EncodeToken(selectorStart); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.CharData(selector.value)); err != nil {
			return err
		}
		if err := enc.EncodeToken(selectorStart.End()); err != nil {
			return err
		}
	}
	return enc.EncodeToken(selectorSetStart.End())
}

func writeTextElement(enc *xml.Encoder, space, local, value string) error {
	start := xml.StartElement{Name: xml.Name{Space: space, Local: local}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if value != "" {
		if err := enc.EncodeToken(xml.CharData(value)); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}

// enumerateBodyはWS-Enumeration Enumerate requestのbodyを書き込む。OptimizeEnumerationにより、多くの実装はPullなしでItemsを返す。
func enumerateBody(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Space: nsWSEnumerate, Local: "Enumerate"}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsWSManagement, "OptimizeEnumeration", ""); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsWSManagement, "MaxElements", "99"); err != nil {
		return err
	}
	return enc.EncodeToken(start.End())
}

func pullBody(enumerationContext string) func(*xml.Encoder) error {
	return func(enc *xml.Encoder) error {
		start := xml.StartElement{Name: xml.Name{Space: nsWSEnumerate, Local: "Pull"}}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		if err := writeTextElement(enc, nsWSEnumerate, "EnumerationContext", enumerationContext); err != nil {
			return err
		}
		if err := writeTextElement(enc, nsWSManagement, "MaxElements", "99"); err != nil {
			return err
		}
		return enc.EncodeToken(start.End())
	}
}

// invokeBodyはWS-Man method invocationのbodyを書き込む。要素名はCIMの慣習に従い"<MethodName>_INPUT"とする。
func invokeBody(resourceURI, method string, params []invokeParam) func(*xml.Encoder) error {
	return func(enc *xml.Encoder) error {
		start := xml.StartElement{Name: xml.Name{Space: resourceURI, Local: method + "_INPUT"}}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		for _, param := range params {
			if err := writeInvokeParam(enc, resourceURI, param); err != nil {
				return err
			}
		}
		return enc.EncodeToken(start.End())
	}
}

func writeInvokeParam(enc *xml.Encoder, resourceURI string, param invokeParam) error {
	if param.reference == nil {
		return writeTextElement(enc, resourceURI, param.name, param.value)
	}

	paramStart := xml.StartElement{Name: xml.Name{Space: resourceURI, Local: param.name}}
	if err := enc.EncodeToken(paramStart); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsAddressing, "Address", param.reference.address); err != nil {
		return err
	}
	refParamsStart := xml.StartElement{Name: xml.Name{Space: nsAddressing, Local: "ReferenceParameters"}}
	if err := enc.EncodeToken(refParamsStart); err != nil {
		return err
	}
	if err := writeTextElement(enc, nsWSManagement, "ResourceURI", param.reference.resourceURI); err != nil {
		return err
	}
	if len(param.reference.selectors) > 0 {
		if err := writeSelectorSet(enc, param.reference.selectors); err != nil {
			return err
		}
	}
	if err := enc.EncodeToken(refParamsStart.End()); err != nil {
		return err
	}
	return enc.EncodeToken(paramStart.End())
}
