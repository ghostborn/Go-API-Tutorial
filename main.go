package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "example/Go-Api-Tutorial/docs"

	"github.com/didip/tollbooth/v6"
	"github.com/didip/tollbooth/v6/limiter"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Book 图书模型
// @Description 图书信息
type Book struct {
	// ID       string `json:"id"`
	gorm.Model
	Title    string `json:"title" validate:"required,min=2,max=50"`
	Author   string `json:"author" validate:"required,min=2"`
	Quantity int    `json:"quantity" validate:"gte=0"` // 库存>=0
}

// BookUpdate 图书更新结构体
type BookUpdate struct {
	Title    string `json:"title" example:"红楼梦"`
	Author   string `json:"author" example:"曹雪芹"`
	Quantity int    `json:"quantity" example:"10"`
}

var jwtSecret = []byte("my_book_123456")

// User 用户模型（数据库表）
type User struct {
	gorm.Model
	Username string `json:"username" validate:"required,min=3,max=10"`
	Password string `json:"password" validate:"required,min=6"`
}

// UserDTO 用户登录注册DTO
// @Description 用于登录和注册
type UserDTO struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	db       *gorm.DB
	validate *validator.Validate
)

// 初始化数据库
func initDB() {
	var err error
	db, err = gorm.Open(sqlite.Open("books.db"), &gorm.Config{})
	if err != nil {
		panic("数据库连接失败：" + err.Error())
	}
	// 自动创建表（没有表就自动建）
	err = db.AutoMigrate(&Book{}, &User{})
	// if !db.Migrator().HasTable(&User{}) {
	// 	db.Migrator().CreateTable(&User{})
	// }

	// fmt.Println("✅ 数据库初始化完成")
	if err != nil {
		panic("建表失败:" + err.Error())
	}
}

// 初始化校验器
func initValidator() {
	validate = validator.New()
}

// 统一成功响应
func successResp(ctx *gin.Context, data any) {
	ctx.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data":    data,
	})
}

// 统一失败响应
func failResp(ctx *gin.Context, code int, msg string) {
	ctx.JSON(code, gin.H{
		"code":    code,
		"message": msg,
		"data":    nil,
	})
}

// 分页获取图书列表
// @Summary 分页查询图书
// @Produce json
// @Param page query int false "页码" default(1)
// @Param size query int false "每页条数" default(10)
// @Success 200 {object} map[string]any "{"code":200,"message":"success","data":{"list":[],"total":0,"page":1,"size":10}}"
// @Router /books [get]
func getBooks(ctx *gin.Context) {
	// 1. 获取 page 和 size 参数（不传默认 page=1, size=10）
	pageStr := ctx.DefaultQuery("page", "1")
	sizeStr := ctx.DefaultQuery("size", "10")

	// 2.转成数字
	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)

	// 3. 计算分页偏移量
	offset := (page - 1) * size

	// 4.查询总数
	var total int64
	db.Model(&Book{}).Count(&total)

	//5.查询当前页数据
	var books []Book
	db.Offset(offset).Limit(size).Find(&books)

	// 6. 返回数据 + 分页信息
	successResp(ctx, gin.H{
		"list":  books,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// 获取单本图书
// @Summary 根据ID获取图书
// @Produce json
// @Param id path int true "图书ID"
// @Success 200 {object} map[string]any "{"code":200,"message":"success","data":{"id":0,"title":"","author":"","quantity":0}}"
// @Failure 404 {object} map[string]any "{"code":404,"message":"图书不存在","data":null}"
// @Router /books/{id} [get]
func getBookById(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		failResp(ctx, http.StatusBadRequest, "ID必须为数字")
		return
	}
	var book Book
	if err := db.First(&book, id).Error; err != nil {
		failResp(ctx, http.StatusNotFound, "图书不存在")
		return
	}
	successResp(ctx, book)
}

// 新增图书
// @Summary 添加图书
// @Security token
// @Accept json
// @Produce json
// @Param book body Book true "图书信息"
// @Success 200 {object} map[string]any
// @Router /books [post]
func createBook(ctx *gin.Context) {
	var book Book
	if err := ctx.ShouldBindJSON(&book); err != nil {
		failResp(ctx, http.StatusBadRequest, "参数格式错误")
		return
	}

	if err := validate.Struct(book); err != nil {
		// 解析validator详细错误
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			failResp(ctx, http.StatusBadRequest, "参数校验失败")
			return
		}
		// 拼接提示
		var tipBuilder strings.Builder
		for _, v := range errs {
			switch v.Field() {
			case "Title":
				tipBuilder.WriteString("书名不能为空且长度合法; ")
			case "Author":
				tipBuilder.WriteString("作者不能为空; ")
			case "Quantity":
				tipBuilder.WriteString("库存不能为负数; ")
			}
		}
		tip := tipBuilder.String()
		failResp(ctx, http.StatusBadRequest, tip)
		return
	}

	// 存入数据库
	if err := db.Create(&book).Error; err != nil {
		failResp(ctx, http.StatusInternalServerError, "创建图书失败")
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{
		"message": "创建成功",
		"book":    book,
	})
}

// 批量新增图书
// @Summary 批量添加多本图书
// @Security token
// @Accept json
// @Produce json
// @Param books body []Book true "图书列表"
// @Success 200 {object} map[string]any
// @Router /books/batch [post]
func batchCreateBooks(ctx *gin.Context) {
	// 定义一个 图书数组（支持接收多本）
	var books []Book

	// 绑定 JSON 数组
	if err := ctx.ShouldBindJSON(&books); err != nil {
		failResp(ctx, http.StatusBadRequest, "批量参数格式错误:"+err.Error())
		return
	}

	// 校验每一本书
	for _, book := range books {
		if err := validate.Struct(book); err != nil {
			failResp(ctx, http.StatusBadRequest, "图书校验失败:"+err.Error())
			return
		}
	}

	// 批量插入
	if err := db.Create(&books).Error; err != nil {
		failResp(ctx, http.StatusInternalServerError, "批量创建图书失败")
		return
	}
	successResp(ctx, gin.H{
		"message": "批量添加成功",
		"count":   len(books),
		"books":   books,
	})
}

// 归还图书
// @Summary 归还图书（库存+1）
// @Security token
// @Produce json
// @Param id query int true "图书ID"
// @Success 200 {object} map[string]any
// @Router /return [patch]
func returnBook(ctx *gin.Context) {
	id, ok := ctx.GetQuery("id")
	if !ok {
		failResp(ctx, http.StatusBadRequest, "缺失query参数id")
		return
	}
	bookID, err := strconv.Atoi(id)
	if err != nil {
		failResp(ctx, http.StatusBadRequest, "ID格式错误")
		return
	}

	var book Book
	if err := db.First(&book, bookID).Error; err != nil {
		failResp(ctx, http.StatusNotFound, "Book not found.")
		return
	}

	book.Quantity += 1
	db.Save(&book)
	successResp(ctx, book)
	// ctx.JSON(http.StatusOK, gin.H{"message": "归还成功", "book": book})
}

// 借阅图书
// @Summary 借阅图书（库存-1）
// @Security token
// @Produce json
// @Param id query int true "图书ID"
// @Success 200 {object} map[string]any
// @Router /checkout [patch]
func checkoutBook(ctx *gin.Context) {
	id, ok := ctx.GetQuery("id")
	if !ok {
		failResp(ctx, http.StatusBadRequest, "Missing id query parameter.")
		return
	}

	bookID, err := strconv.Atoi(id)
	if err != nil {
		failResp(ctx, http.StatusBadRequest, "ID格式错误")
		return
	}
	var book Book
	if err := db.First(&book, bookID).Error; err != nil {
		failResp(ctx, http.StatusNotFound, "Book not found.")
		return
	}

	if book.Quantity <= 0 {
		failResp(ctx, http.StatusBadRequest, "图书库存不足，无法借出")
		return
	}
	book.Quantity--
	db.Save(&book)
	successResp(ctx, book)
}

// 修改图书
// @Summary 更新图书信息
// @Security token
// @Accept json
// @Produce json
// @Param id path int true "图书ID"
// @Param book body BookUpdate true "更新内容"
// @Success 200 {object} map[string]any
// @Router /books/{id} [put]
func updateBook(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		failResp(ctx, http.StatusBadRequest, "id必须为数字")
		return
	}

	// var updateData struct {
	// 	Title    string `json:"title"`
	// 	Author   string `json:"author"`
	// 	Quantity int    `json:"quantity"`
	// }

	var updateData BookUpdate
	if err := ctx.ShouldBindJSON(&updateData); err != nil {
		failResp(ctx, http.StatusBadRequest, "JSON参数格式错误")
		return
	}
	var book Book
	if err := db.First(&book, id).Error; err != nil {
		failResp(ctx, http.StatusNotFound, "图书不存在")
		return
	}

	// 校验更新参数
	if err := validate.Struct(updateData); err != nil {
		failResp(ctx, http.StatusBadRequest, "更新参数不合法")
		return
	}
	//只更新有传的字段
	db.Model(&book).Updates(map[string]any{
		"title":    updateData.Title,
		"author":   updateData.Author,
		"quantity": updateData.Quantity,
	})
	successResp(ctx, book)
}

// 删除图书
// @Summary 删除图书
// @Security token
// @Produce json
// @Param id path int true "图书ID"
// @Success 200 {object} map[string]any
// @Router /books/{id} [delete]
func deleteBook(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		failResp(ctx, http.StatusBadRequest, "id必须为数字")
		return
	}
	var book Book
	if err := db.First(&book, id).Error; err != nil {
		failResp(ctx, http.StatusNotFound, "图书不存在")
		return
	}
	db.Delete(&book)
	successResp(ctx, nil)
}

// 搜索图书
// @Summary 模糊搜索图书
// @Produce json
// @Param keyword query string true "搜索关键词"
// @Success 200 {object} map[string]any "{"code":200,"message":"success","data":{"keyword":"","count":0,"list":[]}}"
// @Router /books/search [get]
func searchBooks(ctx *gin.Context) {
	keyword := ctx.Query("keyword")

	//如果关键词为空，返回全部
	if keyword == "" {
		var books []Book
		db.Find(&books)
		successResp(ctx, books)
		return
	}

	var books []Book
	db.Where("title LIKE ? OR author LIKE ?", "%"+keyword+"%", "%"+keyword+"%").Find(&books)
	successResp(ctx, gin.H{
		"keyword": keyword,
		"count":   len(books),
		"list":    books,
	})

}

func LoggerMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 1.请求开始前：记录开始时间
		startTime := time.Now()
		// 2.等待请求执行完
		ctx.Next()
		// 3.请求结束后：打印日志
		latency := time.Since(startTime)
		method := ctx.Request.Method
		path := ctx.Request.URL.Path
		status := ctx.Writer.Status()

		fmt.Printf("[GIN] %s | %3d | %13v | %-4s %s\n",
			time.Now().Format("2006-01-02 15:04:05"),
			status,
			latency,
			method,
			path,
		)
	}
}

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 从请求头获取 token
		tokenStr := ctx.GetHeader("token")
		if tokenStr == "" {
			failResp(ctx, http.StatusUnauthorized, "请先登录")
			ctx.Abort() // 请求停止
			return
		}

		// 解析token
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			return jwtSecret, nil
		})

		// 验证 token是否有效
		if err != nil || !token.Valid {
			failResp(ctx, http.StatusUnauthorized, "登录已过期或无效")
			ctx.Abort() // 请求停止
			return
		}

		ctx.Next() //放行
	}
}

// 用户注册
// @Summary 用户注册
// @Description 用户名唯一，密码加密存储
// @Accept  json
// @Produce json
// @Param   user  body  UserDTO  true  "注册信息"
// @Success 200 {object} map[string]any "{"code":200,"message":"success","data":"注册成功"}"
// @Failure 400 {object} map[string]any "{"code":400,"message":"用户名已被注册","data":null}"
// @Router /register [post]
func register(ctx *gin.Context) {
	var user User
	if err := ctx.ShouldBindJSON(&user); err != nil {
		failResp(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	if err := validate.Struct(user); err != nil {
		failResp(ctx, http.StatusBadRequest, "校验失败：账号3-10位，密码>=6位")
		return
	}
	// 检查用户名是否已存在
	var count int64
	db.Model(&User{}).Where("username = ?", user.Username).Count(&count)
	if count > 0 {
		failResp(ctx, http.StatusBadRequest, "用户名已被注册")
		return
	}
	// 密码加密
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	user.Password = string(hashedPassword)
	db.Create(&user)
	successResp(ctx, "注册成功")
}

// 用户登录
// @Summary 用户登录
// @Description 登录成功返回 JWT Token
// @Accept  json
// @Produce json
// @Param   user  body  UserDTO  true  "登录信息"
// @Success 200 {object} map[string]any "{"code":200,"message":"success","data":"token字符串"}"
// @Failure 401 {object} map[string]any "{"code":401,"message":"账号或密码错误","data":null}"
// @Router /login [post]
func login(ctx *gin.Context) {
	var user User
	if err := ctx.ShouldBindJSON(&user); err != nil {
		failResp(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	//查询数据库是否存在该用户
	var dbUser User
	// err := db.Where("username = ? AND password = ?", user.Username, user.Password).First(&dbUser).Error
	err := db.Where("username = ?", user.Username).First(&dbUser).Error
	if err != nil {
		failResp(ctx, http.StatusUnauthorized, "账号错误")
		return
	}
	// 解密比对密码
	err = bcrypt.CompareHashAndPassword([]byte(dbUser.Password), []byte(user.Password))
	if err != nil {
		failResp(ctx, http.StatusUnauthorized, "密码错误")
		return
	}

	// 生成 JWT Token
	claims := jwt.MapClaims{
		"id":  dbUser.ID,
		"exp": time.Now().Add(24 * time.Hour).Unix(), // 24小时过期
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString(jwtSecret)
	successResp(ctx, tokenStr)
}

// @title           图书管理系统 API
// @version         1.0
// @description     基于 Gin + GORM 实现的企业级图书管理API
// @host            localhost:8080
// @BasePath        /
// @schemes         http
func main() {
	// 初始化
	initDB()
	initValidator()

	router := gin.Default()

	limiterConfig := tollbooth.NewLimiter(5, &limiter.ExpirableOptions{
		DefaultExpirationTTL: time.Second,
	})

	router.Use(func(ctx *gin.Context) {
		if err := tollbooth.LimitByRequest(limiterConfig, ctx.Writer, ctx.Request); err != nil {
			ctx.JSON(429, gin.H{
				"code":    429,
				"message": "请求过于频繁，请稍后再试",
				"data":    nil,
			})
			ctx.Abort()
			return
		}
	})

	router.Use(LoggerMiddleware())
	router.Use(cors.Default())

	// ===== 公开接口（不用登录）=====

	router.POST("/register", register)
	router.POST("/login", login)
	router.GET("/books", getBooks)
	router.GET("/books/:id", getBookById)
	router.GET("/books/search", searchBooks)

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ===== 需要登录的接口 =====
	authGroup := router.Group("/")
	authGroup.Use(JWTAuthMiddleware())
	{
		authGroup.POST("/books", createBook)
		authGroup.POST("/books/batch", batchCreateBooks)
		authGroup.PATCH("/return", returnBook)
		authGroup.PATCH("/checkout", checkoutBook)
		authGroup.DELETE("/books/:id", deleteBook)
		authGroup.PUT("/books/:id", updateBook)
	}

	router.Run("localhost:8080")
}
