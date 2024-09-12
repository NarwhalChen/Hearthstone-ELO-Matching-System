package main

import (
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"math"
	"sync"
	"time"
)

// Constants definition
const (
	ELO_RESULT_WIN     = 1
	ELO_RESULT_LOSS    = -1
	ELO_RESULT_TIE     = 0
	ELO_RATING_DEFAULT = 1500
	DECREASE_RATE      = 20
)

// Struct definitions with GORM tags
type Card struct {
	ID   int    `gorm:"primaryKey"`
	Name string `gorm:"unique;not null"`
}

type Deck struct {
	ID         int    `gorm:"primaryKey"`
	Name       string `gorm:"unique;not null"`
	Cards      []Card `gorm:"many2many:deck_cards;"`
	CurWin     int
	CurLose    int
	CurWinRate float32
}

type Hero struct {
	ID         int    `gorm:"primaryKey"`
	UserID     int    `gorm:"index"` // For linking with User
	Name       string `gorm:"not null"`
	IsUnlocked bool
	Level      int
	CurWin     int
	CurLose    int
	CurWinRate float32
	CurPt      int
	Decks      []Deck `gorm:"foreignKey:HeroID"`
}

type User struct {
	ID          int `gorm:"primaryKey"`
	IsOnline    bool
	Name        string `gorm:"unique;not null"`
	CurHeroID   int
	AllowedDiff int
	HeroList    []Hero `gorm:"foreignKey:UserID"` // One-to-many relationship between User and Hero
}

type MatchingPool struct {
	minPt         int
	maxPt         int
	MatchingQueue []User
	mu            sync.Mutex
}

var db *gorm.DB
var pools []MatchingPool

func main() {
	// Initialize database connection
	var err error
	db, err = gorm.Open(sqlite.Open("game.db"), &gorm.Config{})
	if err != nil {
		panic("failed to connect to database")
	}

	// Auto migration
	db.AutoMigrate(&Card{}, &Deck{}, &Hero{}, &User{})

	// Initialize matching pools (in-memory only)
	initMatchingPools()

	// Create players and initialize hero lists for each player
	// player1 := createUser("Player1")

	// Start matching threads
	for i := range pools {
		go pools[i].startMatching()
	}

	select {} // Prevent main thread from exiting
}

// Initialize matching pools, kept in memory
func initMatchingPools() {
	pools = []MatchingPool{
		{minPt: 1000, maxPt: 1200, MatchingQueue: []User{}},
		{minPt: 1201, maxPt: 1400, MatchingQueue: []User{}},
		{minPt: 1401, maxPt: 1600, MatchingQueue: []User{}},
	}
}

// Create a new user and initialize their hero list
func createUser(name string) User {
	user := User{
		Name:        name,
		IsOnline:    true,
		AllowedDiff: 0,
	}
	db.Create(&user) // Save user to the database

	// Create default hero list for the user
	heroes := createDefaultHeroes(user.ID)
	for _, hero := range heroes {
		db.Create(&hero) // Save each hero to the database
	}
	return user
}

// Create a default hero list for the user
func createDefaultHeroes(userID int) []Hero {
	return []Hero{
		{UserID: userID, Name: "Druid", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Hunter", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Mage", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Paladin", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Priest", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Rogue", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Shaman", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Warlock", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Warrior", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
		{UserID: userID, Name: "Demon Hunter", IsUnlocked: false, Level: 1, CurWin: 0, CurLose: 0, CurWinRate: 0.0, CurPt: ELO_RATING_DEFAULT},
	}
}

// Print the user's hero list
func printUserHeroes(user User) {
	var heroes []Hero
	db.Where("user_id = ?", user.ID).Find(&heroes)
	fmt.Printf("User: %s's Heroes:\n", user.Name)
	for _, hero := range heroes {
		fmt.Printf("Hero: %s, Level: %d, IsUnlocked: %v, Elo: %d\n", hero.Name, hero.Level, hero.IsUnlocked, hero.CurPt)
	}
}

// Get the current hero's Elo score for a user
func (user *User) getCurHeroPt() int {
	var hero Hero
	db.First(&hero, user.CurHeroID)
	if hero.ID > 0 {
		return hero.CurPt
	}
	return ELO_RATING_DEFAULT // Return default Elo score if no hero is found
}

// Update the current hero's Elo score for a user
func (user *User) updateCurHeroPt(newPt int) {
	var hero Hero
	db.First(&hero, user.CurHeroID)
	if hero.ID > 0 {
		hero.CurPt = newPt
		db.Save(&hero)
	}
}

// Add a user to a matching pool
func addUserToPoll(curUser *User, pool *MatchingPool) {
	curUser.AllowedDiff = 0
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.MatchingQueue = append(pool.MatchingQueue, *curUser)
}

// Start matching logic in the pool
func (curPool *MatchingPool) startMatching() {
	for {
		curPool.mu.Lock()
		curQueue := curPool.MatchingQueue
		if len(curQueue) >= 2 {
			firstUser := curQueue[0]
			curQueue = curQueue[1:]
			for index, matchedUser := range curQueue {
				if firstUser.eloMatch(matchedUser, firstUser.AllowedDiff) {
					curQueue = append(curQueue[:index], curQueue[index+1:]...)
					go gameRoom(firstUser, matchedUser)
					break
				}
			}
			firstUser.AllowedDiff += int(28800.0 / float64(firstUser.getCurHeroPt()))
		}

		curPool.MatchingQueue = curQueue
		curPool.mu.Unlock()
		time.Sleep(1 * time.Second)
	}
}

// Check if two users' Elo scores are within the allowed difference
func (curUser *User) eloMatch(matchingUser User, allowedDiff int) bool {
	eloDiff := abs(curUser.getCurHeroPt() - matchingUser.getCurHeroPt())
	return eloDiff <= allowedDiff
}

// Compute the K value
func (curUser *User) computeK() float64 {
	elo := curUser.getCurHeroPt()
	if elo >= 2400 {
		return 16
	} else if elo >= 2100 {
		return 24
	} else {
		return 36
	}
}

// Update Elo score based on the match result
func (curUser *User) eloCal(opponentPt int, result int) {
	K := curUser.computeK()
	expectedScore := 1.0 / (1.0 + math.Pow(10, float64(opponentPt-curUser.getCurHeroPt())/400))

	var actualScore float64
	switch result {
	case ELO_RESULT_WIN:
		actualScore = 1.0
	case ELO_RESULT_TIE:
		actualScore = 0.5
	case ELO_RESULT_LOSS:
		actualScore = 0.0
	default:
		fmt.Println("Invalid result")
		return
	}

	newPt := float64(curUser.getCurHeroPt()) + K*(actualScore-expectedScore)
	curUser.updateCurHeroPt(int(newPt))
}

// Simulate game room
func gameRoom(user1 User, user2 User) {
	user1.eloCal(user2.getCurHeroPt(), ELO_RESULT_WIN)
	user2.eloCal(user1.getCurHeroPt(), ELO_RESULT_LOSS)
}

// Calculate the absolute value
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
