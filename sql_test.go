package tagsql

import (
	"context"
	"fmt"
	"testing"
)

func TestIfTag(t *testing.T) {
	str := "(activity_id_list len> 0 | (country_id_list len> 0)) | (country_id_list len> 0) |  (city_id_list len> 0) | (activity_template_id_list len> 0) | (schedule_start_time_start != '') | (schedule_start_time_end != '') | (schedule_end_time_start != '') | (schedule_end_time_end != '') | (sort_type == 2) | (sort_type == 3)"
	decode := IfTagDecode{}
	n := &node{}
	_, err := decode.Parse(n, str, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(n.ifTag)
}

func TestParamTag(t *testing.T) {
	str := "(activity_id_list;country_id_list;country_id_list;city_id_list)"
	decode := ParamTagDecode{}
	n := &node{}
	result, err := decode.Parse(n, str, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(result)
}

func TestReplaceTag(t *testing.T) {
	str := "(activity_id_list;country_id_list;country_id_list;city_id_list)"
	decode := ParamTagDecode{}
	n := &node{}
	result, err := decode.Parse(n, str, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(result)
}

func TestDecode(t *testing.T) {
	str := `
<<IF:(user_id_list len> 0); PARAM:(user_id_list)>>
			AND t.user_id in (?)<</>>
		<<IF:(user_name_list len> 0); PARAM:(user_name_list)>>
			AND t.userName_col in (?)<</>>
		<<IF:(user_mobile != ''); PARAM:(user_mobile)>>
			AND t.userMobile_col = ? <</>>`

	result, _, err := decode(str, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(result)

}

type SqlParam struct {
	UserIdList   []int64  `json:"user_id_list"`
	UserNameList []string `json:"user_name_list"`
	UserMobile   string   `json:"user_mobile"`
	StatusList   []int32  `json:"status_list"`
}

func TestAssembleSql(t *testing.T) {
	param := SqlParam{
		UserIdList:   []int64{1, 2, 3},
	}
	str := `
select * from
	order_tbl o
<<IF:(user_name_list len> 0 | user_mobile != '')>>
	left join order_extra_tbl oe<</>>
where
	1 = 1
<<IF:(user_id_list len> 0); PARAM:(user_id_list)>>
	AND o.user_id in (?)<</>>
<<IF:(status_list len> 0); PARAM:(status_list)>>
	o.status in (?)<</>>
<<IF:(user_name_list len> 0); PARAM:(user_name_list)>>
	AND oe.user_name in (?)<</>>
<<IF:(user_mobile != ''); PARAM:(user_mobile)>>
	AND oe.use_mobile = ? <</>>`

	sql, arg, err := AssembleSql(context.Background(), str, param, "json")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sql, "\n", arg)
	if sql != `select * from
	order_tbl o
where
	1 = 1
AND o.user_id in (?,?,?)` {
	t.Fatal("sql:", false)
	}

	expectArg := []int64{1, 2, 3}
	if len(expectArg) != len(arg) {
		t.Fatal("arg:", false)
	}
	for i, v := range arg {
		if argv, ok := v.(int64); !ok || argv != expectArg[i] {
			t.Fatal("arg:", false)
		}
	}
	t.Log("result:", true)

}
