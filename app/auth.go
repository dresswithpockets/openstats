package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/dresswithpockets/openstats/app/password"
	"github.com/dresswithpockets/openstats/app/queries"
	"github.com/dresswithpockets/openstats/app/query"
	"github.com/gofiber/fiber/v2"
	"log"
	"net/mail"
	"slices"
	"unicode"
)

func AuthHandler(c *fiber.Ctx) error {
	currentSession, err := SessionStore.Get(c)
	if err != nil {
		log.Println(err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	sessionUserId, ok := currentSession.GetUserID()
	if !ok {
		return c.Next()
	}

	sessionUser, findErr := Queries.FindUser(c.Context(), sessionUserId)
	if errors.Is(findErr, sql.ErrNoRows) {
		return c.Next()
	}

	if findErr != nil {
		log.Println(findErr)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	Locals.User.Set(c, &sessionUser)
	return c.Next()
}

func RequireAuthHandler(c *fiber.Ctx) error {
	if !Locals.User.Exists(c) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	return c.Next()
}

func RequireAdminAuthHandler(c *fiber.Ctx) error {
	user, ok := Locals.User.Get(c)
	if !ok || !IsAdmin(user) {
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	return c.Next()
}

func ValidDisplayName(displayName string) bool {
	return len(displayName) >= MinDisplayNameLength && len(displayName) <= MaxDisplayNameLength
}

// ValidSlug returns true if all of these rules are followed:
//   - slug is at least MinSlugNameLength and no more than MaxSlugNameLength in length
//   - slug is all lowercase
//   - slug contains only latin characters, numbers, or a dash
func ValidSlug(slug string) bool {
	if len(slug) < MinSlugNameLength || len(slug) > MaxSlugNameLength {
		return false
	}

	for _, r := range []rune(slug) {
		if !unicode.IsLower(r) && !unicode.IsNumber(r) && !unicode.IsLetter(r) && r != '-' {
			return false
		}
	}

	return true
}

// ValidPassword returns true if all of these rules are followed:
//   - password is at least MinPasswordLength and no more than MaxPasswordLength in length
//   - password contains only latin characters, numbers, or some special characters: !@#$%^&*
func ValidPassword(password string) bool {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		return false
	}

	for _, r := range []rune(password) {
		if !unicode.IsNumber(r) && !unicode.IsLetter(r) && !slices.Contains(ValidSlugSpecialCharacters, r) {
			return false
		}
	}

	return true
}

func handleGetLoginView(c *fiber.Ctx) error {
	if Locals.User.Exists(c) {
		// we're already authorized so we can just go back home
		return c.RedirectBack("/")
	}

	// otherwise, we can just render the login form
	return c.Render("login", nil)
}

func handlePostLoginView(c *fiber.Ctx) error {
	currentSession, err := SessionStore.Get(c)
	if err != nil {
		log.Println(err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	type LoginDto struct {
		// Slug is the user's unique username
		Slug     string `json:"slug" form:"slug"`
		Password string `json:"password" form:"password"`
	}

	var loginBody LoginDto
	if bodyErr := c.BodyParser(&loginBody); bodyErr != nil {
		// TODO: return problem json indicating the error
		// TODO: redirect to `/login` with bad request info
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if !ValidSlug(loginBody.Slug) {
		// TODO: return problem json indicating the error
		// TODO: redirect to `/login` with bad request info
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if !ValidPassword(loginBody.Password) {
		// TODO: return problem json indicating the error
		// TODO: redirect to `/login` with bad request info
		return c.SendStatus(fiber.StatusBadRequest)
	}

	result, findErr := Queries.FindUserBySlugWithPassword(c.Context(), loginBody.Slug)
	if findErr != nil {
	}

	if errors.Is(findErr, sql.ErrNoRows) {
		// TODO: redirect to `/login` with username not found or password doesnt match
		return c.SendStatus(fiber.StatusNotFound)
	}

	if findErr != nil {
		log.Println(findErr)
		return c.Status(fiber.StatusInternalServerError).Render("500", nil)
	}

	verifyErr := password.VerifyPassword(loginBody.Password, result.EncodedHash)
	if errors.Is(verifyErr, password.ErrHashMismatch) {
		// TODO: redirect to `/login` with username not found or password doesnt match
		return c.SendStatus(fiber.StatusNotFound)
	}

	if verifyErr != nil {
		log.Println(verifyErr)
		return c.Status(fiber.StatusInternalServerError).Render("500", nil)
	}

	currentSession.SetUserID(result.ID)
	if saveErr := currentSession.Save(); saveErr != nil {
		log.Println(saveErr)
		return c.Status(fiber.StatusInternalServerError).Render("500", nil)
	}

	return c.Redirect("/")
}

func handleGetRegisterView(c *fiber.Ctx) error {
	if Locals.User.Exists(c) {
		// we're already authorized so we can just go back home
		return c.Redirect("/")
	}

	// otherwise, we can just render the login form
	return c.Render("register", nil)
}

var (
	ErrInvalidEmailAddress = errors.New("invalid email address")
	ErrInvalidDisplayName  = errors.New("invalid display name")
	ErrInvalidSlug         = errors.New("invalid slug")
	ErrInvalidPassword     = errors.New("invalid password")
)

func AddNewUser(ctx context.Context, displayName, email string, slug, pass string) (newUser *query.User, err error) {
	if len(email) > 0 {
		_, emailErr := mail.ParseAddress(email)
		if emailErr != nil {
			return nil, ErrInvalidEmailAddress
		}
	}

	if len(displayName) > 0 {
		if !ValidDisplayName(displayName) {
			// TODO: return problem json indicating the error
			// TODO: redirect to `/register` with bad request info
			return nil, ErrInvalidDisplayName
		}
	}

	if !ValidSlug(slug) {
		return nil, ErrInvalidSlug
	}

	if !ValidPassword(pass) {
		return nil, ErrInvalidPassword
	}

	encodedPassword, passwordErr := password.EncodePassword(pass, ArgonParameters)
	if passwordErr != nil {
		log.Println(passwordErr)
		return nil, passwordErr
	}

	return Actions.CreateUser(ctx, slug, encodedPassword, email, displayName)
}

func handlePostRegisterView(c *fiber.Ctx) error {
	if Locals.User.Exists(c) {
		// we're already authorized so we can just go back home
		return c.Redirect("/")
	}

	currentSession, err := SessionStore.Get(c)
	if err != nil {
		log.Println(err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	var registerBody RegisterDto
	if bodyErr := c.BodyParser(&registerBody); bodyErr != nil {
		// TODO: return problem json indicating the error
		// TODO: redirect to `/register` with bad request info
		return c.SendStatus(fiber.StatusBadRequest)
	}

	newUser, newUserError := AddNewUser(
		c.Context(),
		registerBody.DisplayName,
		registerBody.Email,
		registerBody.Slug,
		registerBody.Password,
	)
	if newUserError != nil {
		if errors.Is(newUserError, ErrInvalidEmailAddress) {
			// TODO: return problem json indicating the error
			// TODO: redirect to `/register` with bad request info
			return c.SendStatus(fiber.StatusBadRequest)
		}

		if errors.Is(newUserError, ErrInvalidDisplayName) {
			// TODO: return problem json indicating the error
			// TODO: redirect to `/register` with bad request info
			return c.SendStatus(fiber.StatusBadRequest)
		}

		if errors.Is(newUserError, ErrInvalidSlug) {
			// TODO: return problem json indicating the error
			// TODO: redirect to `/register` with bad request info
			return c.SendStatus(fiber.StatusBadRequest)
		}

		if errors.Is(newUserError, ErrInvalidPassword) {
			// TODO: return problem json indicating the error
			// TODO: redirect to `/register` with bad request info
			return c.SendStatus(fiber.StatusBadRequest)
		}

		if errors.Is(newUserError, queries.ErrSlugAlreadyInUse) {
			return c.SendStatus(fiber.StatusConflict)
		}

		log.Println(newUserError)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	currentSession.SetUserID(newUser.ID)
	if saveErr := currentSession.Save(); saveErr != nil {
		log.Println(saveErr)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Redirect("/")
}

func handleGetLogoutView(c *fiber.Ctx) error {
	if !Locals.User.Exists(c) {
		// we're already logged out, just go back home
		return c.Redirect("/")
	}

	// the logout view will send a logout post request on page load
	return c.Render("logout", nil)
}

func handlePostLogoutView(c *fiber.Ctx) error {
	if !Locals.User.Exists(c) {
		// we're already logged out, just go back home
		return c.Redirect("/")
	}

	currentSession, err := SessionStore.Get(c)
	if err != nil {
		log.Println(err)
		return c.Status(fiber.StatusInternalServerError).Render("500", nil)
	}

	destroyErr := currentSession.Destroy()
	if destroyErr != nil {
		log.Println(destroyErr)
		return c.Status(fiber.StatusInternalServerError).Render("500", nil)
	}

	return c.Redirect("/")
}

func SetupAuthRoutes(router fiber.Router) error {
	rootRoute := router.Group("/")

	rootRoute.Use(AuthHandler)
	rootRoute.Use("/auth/logout", RequireAuthHandler)

	rootRoute.Get("/login", handleGetLoginView)
	rootRoute.Post("/login", handlePostLoginView)
	rootRoute.Get("/register", handleGetRegisterView)
	rootRoute.Post("/register", handlePostRegisterView)
	// since GET requests MUST be idempotent on fly.io, the logout request must be AJAX
	rootRoute.Get("/logout", handleGetLogoutView)
	rootRoute.Post("/logout", handlePostLogoutView)

	return nil
}
