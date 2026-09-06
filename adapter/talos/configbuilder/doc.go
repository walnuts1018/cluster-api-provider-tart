// Package configbuilderは、domain/bootstrapが表現するTalos machine configuration合成の意思決定を、
// siderolabs machinery型を用いた実際のYAML/CEL解釈・merge・生成へ変換する。usecase/bootstrap.ConfigRendererの
// 実装であり、siderolabs machinery型を扱う変換処理はこのpackageに閉じ込める。
package configbuilder
