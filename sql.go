package tagsql

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func AssembleSql(ctx context.Context, sql string, v interface{}, tag string) (string, []interface{}, error) {
	inputArgs, err := transStructToFieldMap(v, tag)
	if err != nil {
		return "", nil, err
	}

	nodeList, _, err := decode(sql, 0)
	if err != nil {
		return "", nil, err
	}

	sqlBuffer := &bytes.Buffer{}
	params := make([]*Field, 0)
	for _, v := range nodeList {
		tmpParam, err := v.Process(sqlBuffer, inputArgs)
		if err != nil {
			return "", nil, err
		}
		params = append(params, tmpParam...)
	}
	sql = sqlBuffer.String()
	sql, args, err := dealInSql(sql, params)
	if err != nil {
		return "", nil, err
	}
	return sql, args, nil
}

func dealInSql(sql string, params []*Field) (string, []interface{}, error) {

	questSliceIndexMap := make(map[int]int, 0)
	//检查 ？个数
	questionNumber := 0
	for i, v := range sql {
		if strings.EqualFold(string("?"), string(v)) {
			questSliceIndexMap[questionNumber] = i
			questionNumber++
		}
	}
	//必须有问号，防止全表扫描
	if questionNumber == 0 || (params == nil || len(params) == 0) ||
		questionNumber != len(params) {
		return "", nil, fmt.Errorf("[dealInSql] '?' number err， or arg number err")
	}

	addLen := 0

	//处理in sql
	args := make([]interface{}, 0)
	for i, v := range params {
		if v.Type == fieldSlice {
			if _, ok := questSliceIndexMap[i]; !ok {
				return "", nil, fmt.Errorf("[dealInSql] find questSliceIndexMap failed")
			}
			questionStrs := ""
			l := len(v.Value)
			for j := 0; j < l; j++ {
				questionStrs = questionStrs + "?"
				if j != l-1 {
					questionStrs = questionStrs + ","
				}

			}
			index := questSliceIndexMap[i] + addLen
			sql = sql[:index] + questionStrs + sql[index+1:]
			addLen = addLen + int(len(questionStrs)) - 1
		}
		args = append(args, v.Value...)
	}
	return sql, args, nil
}

type node struct {
	content    string
	next       []*node
	ifTag      []*iFTag
	paramTag   []*paramTag
	replaceTag []*replaceTag
}

func (n *node) Process(sqlBuffer *bytes.Buffer, args map[string]*Field) ([]*Field, error) {
	//判断iftag
	paramResult := make([]*Field, 0)
	b, err := GetIfTagResult(n.ifTag, args)
	if !b {
		return nil, err
	}
	for _, v := range n.next {
		paramTmp, err := v.Process(sqlBuffer, args)
		if err != nil {
			return nil, err
		}
		paramResult = append(paramResult, paramTmp...)
	}
	for _, v := range n.paramTag {
		param, ok := args[v.name]
		if !ok {
			return nil, fmt.Errorf("param:%v is null, in input", v.name)
		}
		paramResult = append(paramResult, param)
	}

	for _, v := range n.replaceTag {
		paramReplace, ok := args[v.name]
		if !ok {
			return nil, fmt.Errorf("paramReplace:%v is null, in input", v.name)
		}
		if paramReplace.Type != fieldString {
			return nil, fmt.Errorf("paramReplace:%v type is err, in input", v.name)
		}
		strings.Replace(n.content, "{$}", paramReplace.reflectValue.String(), 1)
	}
	if sqlBuffer.Len() != 0 {
		sqlBuffer.WriteRune('\n')
	}
	sqlBuffer.WriteString(n.content)
	return paramResult, nil

}

/*
1,

*/

func tagIsFinish(sql string, index int) (int, bool) {
	for ; index < len(sql); index++ {
		if isBlank(rune(sql[index])) {
			continue
		}
		if sql[index] == ';' {
			return index + 1, true
		}
		if index+1 < len(sql) && sql[index] == '>' && sql[index+1] == '>' {
			return index, true
		}
		return index, false
	}
	return index, false

}

func isBlank(c rune) bool {
	if c == ' ' || c == '\t' || c == '\n' {
		return true
	}
	return false
}
func decode(sql string, index int) ([]*node, int, error) {

	funcParseTagFirst := func(sql string, n *node, index int) (int, error) {
		for index < len(sql) {
			tagDecode, indexTmp, err := getTagDecode(sql, index)
			if err != nil {
				return 0, err
			}
			index, err = tagDecode.Parse(n, sql, indexTmp)
			if err != nil {
				return 0, err
			}
			for i := index; i < len(sql); i++ {
				if sql[i] == ' ' {
					continue
				}
				if index > 0 && sql[index] == '>' && sql[index+1] == '>' {
					return index + 1, nil
				} else {
					break
				}
			}

		}
		return len(sql), nil
	}

	funcContent := func(sql string, index int, n *node) (int, bool, error) {
		firstIndex := 0
		lastIndex := 0
		isContainContent := false
		for ; index < len(sql); index++ {
			if isBlank(rune(sql[index])) {
				continue
			}
			if index > 0 && sql[index] == '<' && sql[index+1] == '<' {
				break
			}
			if firstIndex == 0 {
				firstIndex = index
			}
			lastIndex = index
			isContainContent = true
		}
		n.content = sql[firstIndex:lastIndex+1]
		return index - 1, isContainContent, nil
	}

	isNodeEnd := func(sql string, index int) bool {
		if index > 0 && sql[index+2] == '/' && sql[index] == '<' && sql[index+1] == '<' {
			return true
		}
		return false
	}

	isHalfNodeEnd := func(sql string, index int) (bool, int) {
		for ; index < len(sql); index++ {
			if sql[index] == ' ' {
				continue
			}
			if index+1 < len(sql) && sql[index] == '<' && sql[index+1] == '<' {
				return true, index + 2
			}
			return false, index
		}
		return false, index
	}
	result := make([]*node, 0)
	tagFinish := true
	currentNode := &node{}
	for ; index < len(sql); index++ {

		if isNodeEnd(sql, index) {
			index = index + 5
			result = append(result, currentNode)
			currentNode = &node{}
			tagFinish = true
			continue
		}

		if ok, indexTmp := isHalfNodeEnd(sql, index); ok {
			index = indexTmp
			if !tagFinish {
				subNode, indexTmp, err := decode(sql, index)
				if err != nil {
					return nil, 0, err
				}
				currentNode.next = append(currentNode.next, subNode...)
				index = indexTmp
			}

			if tagFinish {
				indexTmp, err := funcParseTagFirst(sql, currentNode, index)
				if err != nil {
					return nil, 0, err
				}
				index = indexTmp
				tagFinish = false
			}
			continue
		}

		indexTmp, isContainContent, err := funcContent(sql, index, currentNode)
		if err != nil {
			return nil, 0, err
		}
		//没有打标签
		if tagFinish && isContainContent {
			result = append(result, currentNode)
			currentNode = &node{}
		}

		index = indexTmp

	}

	return result, index, nil
}

type tagDecode interface {
	Parse(n *node, sql string, index int) (int, error)
}
type IfTagDecode struct {
}

var specialChar = map[rune]bool{'(': true, ')': true, ',': true, ';': true, '<': true, '>': true}

func (d *IfTagDecode) Parse(n *node, sql string, index int) (int, error) {
	ifTagList, index, err := d.parse(sql, index)
	if err != nil {
		return 0, err
	}
	n.ifTag = ifTagList
	return index, nil
}

func (d *IfTagDecode) parse(sql string, index int) ([]*iFTag, int, error) {
	ptr := -1
	arrayTag := make([]*iFTag, 0)
	current := &iFTag{}
	count := 0
	step := 0

	initTagFunc := func(sql string, index int, tag *iFTag, step int, ptr int) (bool, error) {
		switch step {
		case 0:
			current.paramName = sql[ptr:index]
		case 1:
			if relation, ok := iFRelationMap[sql[ptr:index]]; ok {
				current.relation = relation
			} else {
				return false, fmt.Errorf("[initTagFunc] err fomart:%v", sql[ptr:])
			}

		case 2:
			current.value = sql[ptr:index]
		case 3:
			if sql[index:index] == "&" {
				current.logistic = andLogistic
			}
		}
		return true, nil
	}

	for ; index < len(sql); index++ {
		if isBlank(rune(sql[index])) && ptr == -1 {
			continue
		}

		if indexTmp, ok := tagIsFinish(sql, index); ok {
			arrayTag = append(arrayTag, current)
			if count != 0 || step != 3 {
				return nil, 0, fmt.Errorf("err format ifDecode:%v", sql[index:])
			}
			index = indexTmp
			break
		}

		if sql[index] == '(' {
			if count > 0 {
				tag, indexTmp, err := d.parse(sql, index)
				if err != nil {
					return nil, 0, err
				}
				current.next = append(current.next, tag...)
				index = indexTmp
				step = 3
			} else {
				count++

			}
			continue
		}

		if sql[index] == ')' {
			count--
			//等于）& ptr != -1 时需要截断
			if count < 0 {
				break
			}
			if ptr == -1 {
				continue
			}
		}

		if ptr == -1 {
			ptr = index
			continue
		}

		if ptr != -1 && !(isBlank(rune(sql[index])) || specialChar[rune(sql[index])]) {
			continue
		}

		if ok, err := initTagFunc(sql, index, current, step, ptr); !ok {
			continue
		} else if err != nil {
			return nil, 0, err
		}

		ptr = -1
		step++
		if step == 4 {
			arrayTag = append(arrayTag, current)
			current = &iFTag{}
			step = 0
		}

	}

	return arrayTag, index, nil
}

type ParamTagDecode struct {
}

func (d *ParamTagDecode) Parse(n *node, sql string, index int) (int, error) {
	ptrFirst := -1
	ptrSecond := -1

	for ; index < len(sql); index++ {
		if sql[index] == ' ' {
			continue
		}
		if sql[index] != '(' {
			return 0, fmt.Errorf("err format, :%v", sql[index:])
		}
		index++
		break
	}

	for ; index < len(sql); index++ {

		if sql[index] == ' ' && ptrFirst == -1 {
			continue
		}
		if sql[index] == ' ' && ptrFirst != -1 {
			ptrSecond = index
			continue
		}
		if ptrFirst == -1 {
			ptrFirst = index
		}

		if sql[index] == ',' {
			if ptrSecond == -1 {
				ptrSecond = index
			}
			n.paramTag = append(n.paramTag, &paramTag{name: sql[ptrFirst:ptrSecond]})
			ptrFirst = -1
			ptrSecond = -1
		}
		if sql[index] == ';' {
			index++
			break
		}
		if sql[index] == ')' {
			if ptrFirst != -1 {
				if ptrSecond == -1 {
					ptrSecond = index
				}
				n.paramTag = append(n.paramTag, &paramTag{name: sql[ptrFirst:ptrSecond]})
			}
			index++
			break
		}
	}
	return index, nil
}

type PlaceTagDecode struct {
}

func (d *PlaceTagDecode) Parse(n *node, sql string, index int) (int, error) {
	ptrFirst := -1
	ptrSecond := -1

	for ; index < len(sql); index++ {
		if sql[index] == ' ' {
			continue
		}
		if sql[index] != '(' {
			return 0, fmt.Errorf("err format, :%v", sql[index:])
		}
		index++
		break
	}

	for ; index < len(sql); index++ {

		if sql[index] == ' ' && ptrFirst == -1 {
			continue
		}
		if sql[index] == ' ' && ptrFirst != -1 {
			ptrSecond = index
			continue
		}
		if ptrFirst == -1 {
			ptrFirst = index
		}

		if sql[index] == ';' {
			if ptrSecond == -1 {
				ptrSecond = index
			}
			n.replaceTag = append(n.replaceTag, &replaceTag{name: sql[ptrFirst:ptrSecond]})
			ptrFirst = -1
			ptrSecond = -1
		}
		if sql[index] == ')' {
			if ptrFirst != -1 {
				if ptrSecond == -1 {
					ptrSecond = index
				}
				n.replaceTag = append(n.replaceTag, &replaceTag{name: sql[ptrFirst:ptrSecond]})
			}

			break
		}
	}
	return index, nil
}

func getTagDecode(sql string, index int) (tagDecode, int, error) {

	startIndex := -1
	endIndex := -1
	for ; index < len(sql); index++ {
		if sql[index] == ' ' && startIndex == -1 {
			continue
		}
		if startIndex == -1 {
			startIndex = index
		} else if endIndex == -1 && (sql[index] == ' ' || sql[index] == ':') {
			endIndex = index
		}

		if sql[index] == ':' {
			break
		}
	}

	if index == len(sql) && sql[len(sql)-1] != ':' {
		return nil, 0, fmt.Errorf("not contain tag sql:%v", sql[startIndex:endIndex])
	}

	tagStr := sql[startIndex:endIndex]
	switch tagStr {
	case "IF":
		return &IfTagDecode{}, index + 1, nil
	case "PARAM":
		return &ParamTagDecode{}, index + 1, nil
	case "PLACE":
		return &PlaceTagDecode{}, index + 1, nil
	default:
		return nil, 0, fmt.Errorf("tag err:%v", tagStr)
	}

}

type IFRelation int32

const (
	eQU    IFRelation = iota // ==
	nEQ                      // !=
	lSS                      // <
	lEQ                      // <=
	gTR                      // >
	gEQ                      // >=
	eQULen                   //  len=
	nEQLen                   // len!=
	lSSLen                   // len<
	lEQLen                   // len<=
	gTRLen                   // len>
	gEQLen                   // len>=
)

var iFRelationMap = map[string]IFRelation{
	"==": eQU, "!=": nEQ, "<": lSS, "<=": lEQ, ">": gTR,
	">=": gEQ, "len=": eQULen, "len!=": nEQLen, "len<": lSSLen, "len<=": lEQLen,
	"len>": gTRLen, "len>=": gEQLen,
}

type logistic int32

const (
	andLogistic logistic = 1 // &
	orLogistic  logistic = 0 // |
)

type iFTag struct {
	paramName string
	value     string
	valueType fieldType
	relation  IFRelation
	logistic  logistic
	next      []*iFTag
}

func (d *iFTag) init(args map[string]*Field) error {
	if field, ok := args[d.paramName]; ok {
		d.valueType = field.Type
	}else {
		return fmt.Errorf("ifTag, name:%v init fail", d.paramName)
	}
	if d.valueType == fieldString || d.value == `''` || d.value == `""` {
		d.value = ""
	}
	return nil
}

func GetIfTagResult(ifList []*iFTag, args map[string]*Field) (bool, error) {
	result := true
	var logistic logistic
	var err error
	for i, v := range ifList {
		tmp := true
		if len(v.next) > 0 {
			tmp, err = GetIfTagResult(v.next, args)
		} else {
			tmp, err = v.judge(args)
		}
		if err != nil {
			return false, err
		}

		if i == 0 {
			result = tmp
			logistic = v.logistic
			continue
		}

		if logistic == orLogistic {
			result = result || tmp
		} else {
			result = result && tmp
		}
		logistic = v.logistic
	}
	return result, nil
}

func (f *iFTag) judge(args map[string]*Field) (bool, error) {
	field, ok := args[f.paramName]
	if !ok {
		return false, fmt.Errorf("ifTag, name:%v init fail", f.paramName)
	}
	f.valueType = field.Type
	if f.valueType == fieldString || f.value == `''` || f.value == `""` {
		f.value = ""
	}
	switch f.relation {
	case gTR:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] strconv.Atoi failed:%v", f.paramName)
			}
			rStr := field.reflectValue.Int()
			if rStr > vStr {
				return true, nil
			}
		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] strconv.Atoi failed:%v", f.paramName)
			}
			rStr := field.reflectValue.Float()

			if rStr > vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) > 0 {
				return true, nil
			}
		case fieldSlice:
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)

		}

	case gEQ:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Int()

			if fStr >= vStr {
				return true, nil
			}

		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Float()

			if fStr >= vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) >= 0 {
				return true, nil
			}

		case fieldSlice:
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)

		}

	case lSS:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Int()

			if fStr < vStr {
				return true, nil
			}

		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Float()

			if fStr < vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) < 0 {
				return true, nil
			}

		case fieldSlice:
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)

		}

	case lEQ:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Int()

			if fStr <= vStr {
				return false, nil
			}

		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Float()

			if fStr <= vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) <= 0 {
				return true, nil
			}

		case fieldSlice:
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)

		}

	case eQU:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Int()

			if fStr == vStr {
				return true, nil
			}

		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Float()

			if fStr == vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) == 0 {
				return true, nil
			}

		case fieldSlice:
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)

		}

	case nEQ:
		switch f.valueType {
		case fieldInt:
			vStr, err := strconv.ParseInt(f.value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Int()

			if fStr != vStr {
				return true, nil
			}

		case fieldFloat:
			vStr, err := strconv.ParseFloat(f.value, 64)
			if err != nil {
				return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
			}
			fStr := field.reflectValue.Float()
			if fStr != vStr {
				return true, nil
			}

		case fieldString:
			if strings.Compare(field.reflectValue.String(), f.value) != 0 {
				return true, nil
			}

		case fieldSlice:
			return true, nil

		}

	case gTRLen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() > vLen {
			return true, nil
		}
	case gEQLen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() >= vLen {
			return true, nil
		}
	case lSSLen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() < vLen {
			return true, nil
		}
	case lEQLen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() == vLen {
			return true, nil
		}

	case eQULen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() == vLen {
			return true, nil
		}
	case nEQLen:
		if f.valueType != fieldSlice {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		vLen, err := strconv.Atoi(f.value)
		if err != nil {
			return false, fmt.Errorf("[initIfCond] > not support slice:%v", f.paramName)
		}

		if field.reflectValue.Len() != vLen {
			return true, nil
		}
	}
	return false, nil
}

type paramTag struct {
	name string
}

type replaceTag struct {
	name string
}

type fieldType int

const (
	fieldInt    fieldType = 1
	fieldFloat  fieldType = 2
	fieldString fieldType = 3
	fieldSlice  fieldType = 4
)

type Field struct {
	Name         string
	Value        []interface{}
	reflectValue reflect.Value
	Type         fieldType
	Len          int //数组的时候才有值
}

func transStructToFieldMap(v interface{}, tag string) (map[string]*Field, error) {
	if tag == "" {
		return nil, fmt.Errorf("tage is null")
	}

	t := reflect.TypeOf(v)
	r := reflect.ValueOf(v)
	if t.Kind() == reflect.Ptr {
		r = r.Elem()
		t = t.Elem()
	}
	l := t.NumField()

	m := make(map[string]*Field, l)

	for i := 0; i < l; i++ {
		var field Field

		field.Name = t.Field(i).Tag.Get(tag)
		field.reflectValue = r.Field(i)
		switch r.Field(i).Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			field.Value = append(field.Value, r.Field(i).Interface())
			field.Type = fieldInt
		case reflect.Float32, reflect.Float64:
			field.Value = append(field.Value, r.Field(i).Interface())
			field.Type = fieldFloat
		case reflect.String:
			field.Value = append(field.Value, r.Field(i).Interface())
			field.Type = fieldString
		case reflect.Slice:
			field.Type = fieldSlice
			len := r.Field(i).Len()
			for j := 0; j < len; j++ {
				k := r.Field(i).Index(j).Kind()
				switch k {
				case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:

				case reflect.Float32, reflect.Float64:

				case reflect.String:

				default:
					return nil, fmt.Errorf("unsupport type:%v", t.Field(i).Type.PkgPath())
				}
				field.Value = append(field.Value, r.Field(i).Index(j).Interface())
			}
		}

		m[field.Name] = &field

	}
	return m, nil
}
