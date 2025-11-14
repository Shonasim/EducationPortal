package main

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB
var tpl *template.Template

func main() {
	// === БД ===
	var err error
	db, err = sql.Open("sqlite3", "./edu.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

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

	r.Run(":8080")
}

// === Инициализация БД ===
func initDB() {
	// Пользователи
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE,
		password TEXT,
		name TEXT,
		role TEXT DEFAULT 'student'
	)`)

	// Курсы (с ссылкой)
	db.Exec(`CREATE TABLE IF NOT EXISTS courses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		description TEXT,
		link TEXT
	)`)

	// Уроки
	db.Exec(`CREATE TABLE IF NOT EXISTS lessons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		course_id INTEGER,
		title TEXT,
		content TEXT,
		order_num INTEGER,
		FOREIGN KEY (course_id) REFERENCES courses(id) ON DELETE CASCADE
	)`)

	// Записи
	db.Exec(`CREATE TABLE IF NOT EXISTS enrollments (
		user_id INTEGER,
		course_id INTEGER,
		PRIMARY KEY (user_id, course_id)
	)`)

	// Админ по умолчанию
	var count int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "admin@edu.com").Scan(&count)
	if count == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), 14)
		db.Exec("INSERT INTO users (email, password, name, role) VALUES (?, ?, ?, ?)",
			"admin@edu.com", string(hash), "Админ", "admin")
	}
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
	db.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role)
	if role != "admin" {
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
	var role string
	db.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role)
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

	hash, _ := bcrypt.GenerateFromPassword([]byte(password), 14)

	_, err := db.Exec("INSERT INTO users (email, password, name) VALUES (?, ?, ?)", email, string(hash), name)
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

	// Имя
	var name string
	db.QueryRow("SELECT name FROM users WHERE id = ?", userID).Scan(&name)

	// Все курсы
	rows, _ := db.Query("SELECT id, title FROM courses")
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
	rows2, _ := db.Query("SELECT course_id FROM enrollments WHERE user_id = ?", userID)
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

	// Уроки
	rows, _ := db.Query("SELECT id, title, content FROM lessons WHERE course_id = ? ORDER BY order_num", courseID)
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
	rows, _ := db.Query("SELECT id, title, description, link FROM courses")
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
	c.String(http.StatusOK, "")
}
