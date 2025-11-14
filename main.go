package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB
var tpl *template.Template

func main() {
	// === Выводим путь ===
	wd, _ := os.Getwd()
	fmt.Printf("Запуск из директории: %s\n", wd)
	fmt.Println("Попытка открыть/создать БД: ./edu.db")

	// === Открываем БД с проверкой ===
	var err error
	dbPath := "./edu.db"
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("ОШИБКА: Не удалось открыть БД: %v", err)
	}
	defer db.Close()

	// === Проверяем, можем ли писать в файл ===
	if err := testDBWrite(dbPath); err != nil {
		log.Fatalf("ОШИБКА: Нет прав на запись в директорию! %v", err)
	}

	// === Инициализация БД ===
	initDB()

	// === Шаблоны ===
	tpl = template.Must(template.ParseGlob("templates/*.html"))

	// === Gin ===
	r := gin.Default()
	r.SetHTMLTemplate(tpl)

	// Публичные
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/login") })
	r.GET("/login", loginPage)
	r.POST("/login", login)
	r.GET("/register", registerPage)
	r.POST("/register", register)
	r.GET("/logout", logout)

	// Ученик
	r.GET("/dashboard", auth, dashboard)
	r.POST("/enroll/:id", auth, enroll)
	r.POST("/unenroll/:id", auth, unenroll)

	// Курс
	r.GET("/course/:id", auth, coursePage)
	r.POST("/course/:id/lesson", authAdmin, addLesson)

	// Админ
	r.GET("/admin", authAdmin, adminPanel)
	r.POST("/admin/course", authAdmin, addCourse)
	r.POST("/admin/course/edit/:id", authAdmin, editCourse)
	r.POST("/admin/course/delete/:id", authAdmin, deleteCourse)

	// === Запуск сервера ===
	fmt.Println("Сервер запускается на http://localhost:8080")
	fmt.Println("Если появится окно Windows — нажмите «Разрешить доступ»")
	fmt.Println("После этого откройте браузер: http://localhost:8080")

	// log.Fatal — покажет ошибку, если порт занят или заблокирован
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("СЕРВЕР НЕ ЗАПУСТИЛСЯ: %v", err)
	}
}

// === Проверка прав на запись ===
func testDBWrite(path string) error {
	absPath := path
	if !filepath.IsAbs(path) {
		wd, _ := os.Getwd()
		absPath = filepath.Join(wd, path)
	}
	fmt.Printf("Проверка записи: %s\n", absPath)

	f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("не могу создать файл БД: %v", err)
	}
	f.Close()
	fmt.Println("Проверка записи пройдена")
	return nil
}

// === Инициализация БД с проверкой ошибок ===
func initDB() {
	fmt.Println("Инициализация таблиц...")

	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE,
			password TEXT,
			name TEXT,
			role TEXT DEFAULT 'student'
		)`,
		`CREATE TABLE IF NOT EXISTS courses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			description TEXT,
			link TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS lessons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			course_id INTEGER,
			title TEXT,
			content TEXT,
			order_num INTEGER,
			FOREIGN KEY (course_id) REFERENCES courses(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS enrollments (
			user_id INTEGER,
			course_id INTEGER,
			PRIMARY KEY (user_id, course_id)
		)`,
	}

	for _, sql := range tables {
		if _, err := db.Exec(sql); err != nil {
			log.Fatalf("ОШИБКА создания таблицы:\nSQL: %s\nОшибка: %v", sql, err)
		}
	}

	// Админ по умолчанию
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "admin@edu.com").Scan(&count)
	if err != nil {
		log.Fatalf("ОШИБКА проверки админа: %v", err)
	}
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), 14)
		if err != nil {
			log.Fatalf("ОШИБКА хеширования пароля: %v", err)
		}
		_, err = db.Exec("INSERT INTO users (email, password, name, role) VALUES (?, ?, ?, ?)",
			"admin@edu.com", string(hash), "Админ", "admin")
		if err != nil {
			log.Fatalf("ОШИБКА создания админа: %v", err)
		}
		fmt.Println("Создан администратор:")
		fmt.Println("   Логин: admin@edu.com")
		fmt.Println("   Пароль: admin123")
	} else {
		fmt.Println("Администратор уже существует")
	}

	fmt.Println("База данных успешно инициализирована")
}

// === Аутентификация ===
func auth(c *gin.Context) {
	if getUserID(c) == 0 {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	c.Next()
}

func authAdmin(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role)
	if err != nil || role != "admin" {
		c.String(http.StatusForbidden, "Доступ запрещён")
		c.Abort()
		return
	}
	c.Next()
}

func getUserID(c *gin.Context) int {
	cookie, err := c.Cookie("user_id")
	if err != nil {
		return 0
	}
	id, _ := strconv.Atoi(cookie)
	return id
}

func isAdmin(userID int) bool {
	if userID == 0 {
		return false
	}
	var role string
	_ = db.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role)
	return role == "admin"
}

// === Страницы ===
func loginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{"error": c.Query("error")})
}

func registerPage(c *gin.Context) {
	c.HTML(http.StatusOK, "register.html", gin.H{"error": c.Query("error")})
}

// === Регистрация / Вход / Выход ===
func register(c *gin.Context) {
	email := c.PostForm("email")
	password := c.PostForm("password")
	name := c.PostForm("name")

	if email == "" || password == "" || name == "" {
		c.Redirect(http.StatusSeeOther, "/register?error=Заполните+все+поля")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/register?error=Ошибка+серверa")
		return
	}

	_, err = db.Exec("INSERT INTO users (email, password, name) VALUES (?, ?, ?)", email, string(hash), name)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/register?error=Email+уже+занят")
		return
	}
	c.Redirect(http.StatusSeeOther, "/login")
}

func login(c *gin.Context) {
	email := c.PostForm("email")
	password := c.PostForm("password")

	var id int
	var hash, role string
	err := db.QueryRow("SELECT id, password, role FROM users WHERE email = ?", email).
		Scan(&id, &hash, &role)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		c.Redirect(http.StatusSeeOther, "/login?error=Неверный+логин+или+пароль")
		return
	}

	c.SetCookie("user_id", strconv.Itoa(id), 3600*24, "/", "", false, true)
	if role == "admin" {
		c.Redirect(http.StatusSeeOther, "/admin")
	} else {
		c.Redirect(http.StatusSeeOther, "/dashboard")
	}
}

func logout(c *gin.Context) {
	c.SetCookie("user_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusSeeOther, "/login")
}

// === Личный кабинет ===
func dashboard(c *gin.Context) {
	userID := getUserID(c)

	var name string
	if err := db.QueryRow("SELECT name FROM users WHERE id = ?", userID).Scan(&name); err != nil {
		c.String(http.StatusInternalServerError, "Ошибка загрузки профиля")
		return
	}

	// Все курсы
	rows, err := db.Query("SELECT id, title FROM courses")
	if err != nil {
		c.String(http.StatusInternalServerError, "Ошибка загрузки курсов")
		return
	}
	defer rows.Close()
	var allCourses []map[string]any
	for rows.Next() {
		var id int
		var title string
		rows.Scan(&id, &title)
		allCourses = append(allCourses, map[string]any{"ID": id, "Title": title})
	}

	// Записанные
	var myCourses []map[string]any
	var enrolledIDs []int
	rows2, err := db.Query("SELECT course_id FROM enrollments WHERE user_id = ?", userID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Ошибка загрузки записей")
		return
	}
	defer rows2.Close()
	for rows2.Next() {
		var cid int
		rows2.Scan(&cid)
		enrolledIDs = append(enrolledIDs, cid)

		var title string
		db.QueryRow("SELECT title FROM courses WHERE id = ?", cid).Scan(&title)
		myCourses = append(myCourses, map[string]any{"ID": cid, "Title": title})
	}

	// Доступные
	var availableCourses []map[string]any
	for _, course := range allCourses {
		id := course["ID"].(int)
		isEnrolled := false
		for _, eid := range enrolledIDs {
			if eid == id {
				isEnrolled = true
				break
			}
		}
		if !isEnrolled {
			availableCourses = append(availableCourses, course)
		}
	}

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"MyCourses":        myCourses,
		"AvailableCourses": availableCourses,
		"UserID":           userID,
		"UserName":         name,
		"MyCount":          len(myCourses),
		"AvailCount":       len(availableCourses),
	})
}

func enroll(c *gin.Context) {
	userID := getUserID(c)
	courseID, _ := strconv.Atoi(c.Param("id"))
	db.Exec("INSERT OR IGNORE INTO enrollments (user_id, course_id) VALUES (?, ?)", userID, courseID)

	var title string
	db.QueryRow("SELECT title FROM courses WHERE id = ?", courseID).Scan(&title)
	c.HTML(http.StatusOK, "course_enrolled.html", gin.H{"ID": courseID, "Title": title})
}

func unenroll(c *gin.Context) {
	userID := getUserID(c)
	courseID, _ := strconv.Atoi(c.Param("id"))
	db.Exec("DELETE FROM enrollments WHERE user_id = ? AND course_id = ?", userID, courseID)

	var title string
	db.QueryRow("SELECT title FROM courses WHERE id = ?", courseID).Scan(&title)
	c.HTML(http.StatusOK, "course_available.html", gin.H{"ID": courseID, "Title": title})
}

// === Страница курса ===
func coursePage(c *gin.Context) {
	courseID, _ := strconv.Atoi(c.Param("id"))
	userID := getUserID(c)

	var title, desc, link string
	err := db.QueryRow("SELECT title, description, link FROM courses WHERE id = ?", courseID).
		Scan(&title, &desc, &link)
	if err != nil {
		c.String(http.StatusNotFound, "Курс не найден")
		return
	}

	rows, err := db.Query("SELECT id, title, content FROM lessons WHERE course_id = ? ORDER BY order_num", courseID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Ошибка загрузки уроков")
		return
	}
	defer rows.Close()
	var lessons []map[string]any
	for rows.Next() {
		var id int
		var ltitle, content string
		rows.Scan(&id, &ltitle, &content)
		lessons = append(lessons, map[string]any{"ID": id, "Title": ltitle, "Content": content})
	}

	c.HTML(http.StatusOK, "course.html", gin.H{
		"CourseID":    courseID,
		"CourseTitle": title,
		"Description": desc,
		"Link":        link,
		"Lessons":     lessons,
		"IsAdmin":     isAdmin(userID),
	})
}

func addLesson(c *gin.Context) {
	courseID, _ := strconv.Atoi(c.Param("id"))
	title := c.PostForm("title")
	content := c.PostForm("content")

	var maxOrder int
	db.QueryRow("SELECT COALESCE(MAX(order_num), 0) FROM lessons WHERE course_id = ?", courseID).Scan(&maxOrder)
	db.Exec("INSERT INTO lessons (course_id, title, content, order_num) VALUES (?, ?, ?, ?)",
		courseID, title, content, maxOrder+1)

	c.Redirect(http.StatusSeeOther, "/course/"+c.Param("id"))
}

// === Админка ===
func adminPanel(c *gin.Context) {
	rows, err := db.Query("SELECT id, title, description, link FROM courses")
	if err != nil {
		c.String(http.StatusInternalServerError, "Ошибка загрузки курсов")
		return
	}
	defer rows.Close()
	var courses []map[string]any
	for rows.Next() {
		var id int
		var title, desc, link string
		rows.Scan(&id, &title, &desc, &link)
		courses = append(courses, map[string]any{
			"ID":    id,
			"Title": title,
			"Desc":  desc,
			"Link":  link,
		})
	}

	c.HTML(http.StatusOK, "admin.html", gin.H{"Courses": courses})
}

func addCourse(c *gin.Context) {
	title := c.PostForm("title")
	desc := c.PostForm("description")
	link := c.PostForm("link")
	if title == "" {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	db.Exec("INSERT INTO courses (title, description, link) VALUES (?, ?, ?)", title, desc, link)
	c.Redirect(http.StatusSeeOther, "/admin")
}

func editCourse(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	title := c.PostForm("title")
	desc := c.PostForm("description")
	link := c.PostForm("link")
	db.Exec("UPDATE courses SET title = ?, description = ?, link = ? WHERE id = ?", title, desc, link, id)
	c.Redirect(http.StatusSeeOther, "/admin")
}

func deleteCourse(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	db.Exec("DELETE FROM courses WHERE id = ?", id)
	db.Exec("DELETE FROM enrollments WHERE course_id = ?", id)
	c.String(http.StatusOK, "OK")
}
