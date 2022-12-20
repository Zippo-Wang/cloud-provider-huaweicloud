package model

import (
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/utils"

	"strings"
)

// 云服务按需转包请求体
type ChangeToPeriodReq struct {

	// 待按需转包IP列表
	PublicipIds []string `json:"publicip_ids"`

	// 按需转包周期参数
	ExtendParam *interface{} `json:"extendParam"`
}

func (o ChangeToPeriodReq) String() string {
	data, err := utils.Marshal(o)
	if err != nil {
		return "ChangeToPeriodReq struct{}"
	}

	return strings.Join([]string{"ChangeToPeriodReq", string(data)}, " ")
}
