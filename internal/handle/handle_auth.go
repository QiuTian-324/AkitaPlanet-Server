package handle

import (
	"bytes"
	"errors"
	"fmt"
	g "gin-blog/internal/global"
	"gin-blog/internal/model"
	"gin-blog/internal/utils"
	"gin-blog/internal/utils/jwt"
	"html/template"
	"log/slog"
	"math/rand"
	"strconv"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserAuth struct{}

type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterReq struct {
	Username string `json:"username" form:"username" binding:"required"`
	Password string `json:"password" form:"password" binding:"required,min=4,max=20"`
	Code     string `json:"code" form:"code" binding:"required"`
}

type CodeReq struct {
	Username string `json:"username" form:"username" binding:"required"`
}

type LoginVO struct {
	model.UserInfo

	// 点赞 Set: 用于记录用户点赞过的文章, 评论
	ArticleLikeSet []string `json:"article_like_set"`
	CommentLikeSet []string `json:"comment_like_set"`
	Token          string   `json:"token"`
}

type RetrievePwd struct {
	SiteName     string
	UserName     string
	UserCode     string
	UserCodeTime int
	SiteAddr     string
}

// @Summary 登录
// @Description 登录
// @Tags UserAuth
// @Param form body LoginReq true "登录"
// @Accept json
// @Produce json
// @Success 0 {object} Response[model.LoginVO]
// @Router /login [post]
func (*UserAuth) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		ReturnError(c, g.ErrRequest, err)
		return
	}

	db := GetDB(c)
	rdb := GetRDB(c)

	userAuth, err := model.GetUserAuthInfoByName(db, req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ReturnError(c, g.ErrUserNotExist, nil)
			return
		}
		ReturnError(c, g.ErrDbOp, err)
		return
	}

	// 检查密码是否正确
	if !utils.BcryptCheck(req.Password, userAuth.Password) {
		ReturnError(c, g.ErrPassword, nil)
		return
	}

	// 获取 IP 相关信息 FIXME: 好像无法读取到 ip 信息
	ipAddress := utils.IP.GetIpAddress(c)
	ipSource := utils.IP.GetIpSourceSimpleIdle(ipAddress)

	userInfo, err := model.GetUserInfoById(db, userAuth.UserInfoId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ReturnError(c, g.ErrUserNotExist, nil)
			return
		}
		ReturnError(c, g.ErrDbOp, err)
		return
	}

	roleIds, err := model.GetRoleIdsByUserId(db, userAuth.ID)
	if err != nil {
		ReturnError(c, g.ErrDbOp, err)
		return
	}

	articleLikeSet, err := rdb.SMembers(rctx, g.ARTICLE_USER_LIKE_SET+strconv.Itoa(userAuth.ID)).Result()
	if err != nil {
		ReturnError(c, g.ErrDbOp, err)
		return
	}
	commentLikeSet, err := rdb.SMembers(rctx, g.COMMENT_USER_LIKE_SET+strconv.Itoa(userAuth.ID)).Result()
	if err != nil {
		ReturnError(c, g.ErrDbOp, err)
		return
	}

	// 登录信息正确, 生成 Token

	// UUID 生成方法: ip + 浏览器信息 + 操作系统信息
	// uuid := utils.MD5(ipAddress + browser + os)
	conf := g.Conf.JWT
	token, err := jwt.GenToken(conf.Secret, conf.Issuer, int(conf.Expire), userAuth.ID, roleIds)
	if err != nil {
		ReturnError(c, g.ErrTokenCreate, err)
		return
	}

	// 更新用户验证信息: ip 信息 + 上次登录时间
	err = model.UpdateUserLoginInfo(db, userAuth.ID, ipAddress, ipSource)
	if err != nil {
		ReturnError(c, g.ErrDbOp, err)
		return
	}

	slog.Info("用户登录成功: " + userAuth.Username)

	session := sessions.Default(c)
	session.Set(g.CTX_USER_AUTH, userAuth.ID)
	session.Save()

	// 删除 Redis 中的离线状态
	offlineKey := g.OFFLINE_USER + strconv.Itoa(userAuth.ID)
	rdb.Del(rctx, offlineKey).Result()

	ReturnSuccess(c, LoginVO{
		UserInfo: *userInfo,

		ArticleLikeSet: articleLikeSet,
		CommentLikeSet: commentLikeSet,
		Token:          token,
	})
}

// @Summary 注册
// @Description 注册
// @Tags UserAuth
// @Param form body RegisterReq true "注册"
// @Accept json
// @Produce json
// @Success 0 {object} string
// @Router /register [post]
func (*UserAuth) Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		ReturnError(c, g.ErrRequest, err)
		return
	}
	db := GetDB(c)
	rdb := GetRDB(c)
	result, err := rdb.Get(c, req.Username).Result()
	if err != nil {
		ReturnError(c, g.ErrVerificationCodeExpired, "验证码已过期")
		return
	}
	// 判断验证码是否相等
	if req.Code == result {
		ReturnError(c, g.ErrVerificationCode, "验证码错误")
		return
	}
	// 默认生成一个 super admin 用户
	hashPassword, err := utils.BcryptHash(req.Password)
	if err != nil {
		ReturnError(c, g.ErrVerificationCode, "注册失败")
		return
	}

	// 获取 IP 相关信息 FIXME: 好像无法读取到 ip 信息
	ipAddress := utils.IP.GetIpAddress(c)
	ipSource := utils.IP.GetIpSourceSimpleIdle(ipAddress)
	userAuth := &model.UserAuth{
		Username:  req.Username,
		Password:  hashPassword,
		LoginType: 1,
		IpSource:  ipSource,
		IpAddress: ipAddress,
		UserInfo: &model.UserInfo{
			Nickname: req.Username,
			Avatar:   "https://cdn.hahacode.cn/config/superadmin_avatar.jpg",
			Intro:    "这个人很懒，什么都没有留下",
			Email:    req.Username,
			Website:  "https://www.hahacode.cn",
		},
	}
	// 获取角色role
	var role model.Role

	err = db.Model(&role).Take(&role, "name = ?", "普通用户").Error
	if err != nil {
		ReturnError(c, g.ErrRegister, "注册失败")
	}

	if err = db.Create(&userAuth).Error; err != nil {
		ReturnError(c, g.ErrRegister, "注册失败")
		return
	}

	//  创建用户角色关联表
	db.Create(&model.UserAuthRole{UserAuthId: userAuth.ID, RoleId: role.ID})

	ReturnSuccess(c, role)
}

// @Summary 退出登录
// @Description 退出登录
// @Tags UserAuth
// @Accept json
// @Produce json
// @Success 0 {object} string
// @Router /logout [post]
func (*UserAuth) Logout(c *gin.Context) {
	c.Set(g.CTX_USER_AUTH, nil)

	// 已经退出登录
	auth, _ := CurrentUserAuth(c)
	if auth == nil {
		ReturnSuccess(c, nil)
		return
	}

	session := sessions.Default(c)
	session.Delete(g.CTX_USER_AUTH)
	session.Save()

	// 删除 Redis 中的在线状态
	rdb := GetRDB(c)
	onlineKey := g.ONLINE_USER + strconv.Itoa(auth.ID)
	rdb.Del(rctx, onlineKey)

	ReturnSuccess(c, nil)
}

// @Summary 发送邮箱验证码
// @Description 发送邮箱验证码
// @Tags UserAuth
// @Param email query string true "邮箱"
// @Accept json
// @Produce json
// @Success 0 {object} string
// @Router /code [get]
func (*UserAuth) SendCode(c *gin.Context) {
	var (
		req  CodeReq
		code string
		html bytes.Buffer // 缓存html模板
	)

	if err := c.ShouldBindQuery(&req); err != nil {
		ReturnError(c, g.ErrRequest, err)
		return
	}

	rdb := GetRDB(c)
	// 解析html文件
	//dir, _ := os.Getwd()
	//dir = dir + "\\templates\\email.html"
	temp, err := template.ParseFiles("D:\\AkitaCode\\cloneProject\\gin-vue-blog\\gin-blog-server\\templates\\email.html")
	if err != nil {
		ReturnError(c, g.ErrMailSend, err)
		return
	}

	// 生产随机验证码，将验证码存入redis并设置过期时间
	// 生产随机验证码，将验证码存入redis并设置过期时间
	rand.Seed(time.Now().UnixNano()) // 使用当前时间作为随机数种子
	for i := 0; i < 6; i++ {
		code += fmt.Sprintf("%d", rand.Intn(10)) // 生成 0 到 9 之间的随机数字
	}
	//构造数据渲染模板
	data := RetrievePwd{
		//SiteName:     "",
		//UserName:     "aa",
		UserCode:     code,
		UserCodeTime: g.Conf.Captcha.ExpireTime,
		//SiteAddr:     "https://www.liwenzhou.com/",
	}
	err = temp.Execute(&html, data)
	if err != nil {
		ReturnError(c, g.ErrMailSend, err)
		return
	}
	// 将验证码存入redis
	err = rdb.Set(rctx, req.Username, code, time.Duration(g.Conf.Captcha.ExpireTime)*time.Minute).Err() // 设置验证码，有效期5分钟
	if err != nil {
		ReturnError(c, g.ErrMailSend, err)
		return
	}
	err = utils.InitEmail(req.Username, "注册账号", html.String())
	if err != nil {
		ReturnError(c, g.ErrMailSend, err)
		return
	}
	ReturnSuccess(c, "发送邮箱验证码")
}

// TODO: refresh token
