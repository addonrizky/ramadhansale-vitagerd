package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"net/http"
	"net/url"

	"golang.org/x/crypto/ssh"
)

var apiHost string

type ViaSSHDialer struct {
	client *ssh.Client
}

func (self *ViaSSHDialer) Dial(addr string) (net.Conn, error) {
	return self.client.Dial("tcp", addr)
}

type RespLanggengGetCmpgnByCode struct {
	Campaigns []struct {
		Campaign
	} `json:"campaigns"`
}

type Campaign struct {
	ID                     int       `json:"id"`
	UserID                 int       `json:"user_id"`
	Title                  string    `json:"title"`
	Description            string    `json:"description"`
	Code                   string    `json:"code"`
	CustomFields           string    `json:"custom_fields"`
	StartTime              string    `json:"start_time"`
	EndTime                string    `json:"end_time"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	CampaignUnsubscribeIds any       `json:"campaign_unsubscribe_ids"`
}

type Audience struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Phone  string `json:"phone_number"`
	UserID string `json:"user_id"`
}

func main() {
	currIter, err := getCurrentIteration()
	if err != nil {
		return
	}

	apiHost = os.Getenv("API_LANGGENG_URL")

	//sshHost := os.Getenv("SSH_CRMLITE_HOST") // SSH Server Hostname/IP
	//sshPort := os.Getenv("SSH_CRMLITE_PORT") // SSH Port
	//sshUser := os.Getenv("SSH_CRMLITE_USER") // SSH Username
	//sshPass := os.Getenv("SSH_CRMLITE_PASS") // Empty string for no password
	dbUser := os.Getenv("CRMLITE_DB_USER") // DB username
	dbPass := os.Getenv("CRMLITE_DB_PASS") // DB Password
	dbHost := os.Getenv("CRMLITE_DB_HOST") // DB Hostname/IP
	dbName := os.Getenv("CRMLITE_DB_NAME") // Database name

	limitProcessPerExecutionRaw := os.Getenv("LIMIT_PROCESS_PER_EXECUTION")
	limitProcessPerExecution, err := strconv.Atoi(limitProcessPerExecutionRaw)
	if err != nil {
		fmt.Println("fail pars limit process per execution")
		return
	}

	//var agentClient agent.Agent
	//// Establish a connection to the local ssh-agent
	//conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	//if err != nil {
	//	fmt.Println("error on dialing ssh unix : ", err)
	//	return
	//}
	//defer conn.Close()
	//
	//// Create a new instance of the ssh agent
	//agentClient = agent.NewClient(conn)
	//
	//// The client configuration with configuration option to use the ssh-agent
	//sshConfig := &ssh.ClientConfig{
	//	User:            sshUser,
	//	Auth:            []ssh.AuthMethod{},
	//	HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	//}
	//
	//// When the agentClient connection succeeded, add them as AuthMethod
	//if agentClient == nil {
	//	fmt.Println("agent client connecton not succes")
	//	return
	//}
	//
	//sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeysCallback(agentClient.Signers))
	//// When there's a non empty password add the password AuthMethod
	//if sshPass != "" {
	//	sshConfig.Auth = append(sshConfig.Auth, ssh.PasswordCallback(func() (string, error) {
	//		return sshPass, nil
	//	}))
	//}
	//
	//// Connect to the SSH Server
	//sshcon, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", sshHost, sshPort), sshConfig)
	//if err != nil {
	//	fmt.Println("fail on connect to SSH Server : ", err)
	//	return
	//}
	//
	//defer sshcon.Close()
	//
	//// Now we register the ViaSSHDialer with the ssh connection as a parameter
	//mysql.RegisterDial("mysql+tcp", (&ViaSSHDialer{sshcon}).Dial)

	// And now we can use our new driver with the regular mysql connection string tunneled through the SSH connection
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@mysql+tcp(%s)/%s", dbUser, dbPass, dbHost, dbName))
	if err != nil {
		fmt.Println("fail on connect regular mysql connection tunneled through SSH connection : ", err)
		return
	}

	fmt.Printf("Successfully connected to the db\n")

	apiKey := os.Args[1]
	cmpgnCodeFrom := os.Args[2]
	cpmgnCodeDest := os.Args[3]

	// get user by apikey
	userId, err := getUserIdByApiKey(db, apiKey)
	if err != nil {
		fmt.Println("error dapetin user: ", err)
		return
	}

	// get campaignID source
	campaignIdSource, err := getCampaignIdByCodeUserid(db, cmpgnCodeFrom, userId)
	if err != nil {
		fmt.Println("error ah: ", err)
		return
	}

	// get 20 audience from campaign source
	// select * from campaign_audiences a
	// JOIN audiences b ON a.audience_id = b.id
	// WHERE a.campaign_id = $campaignId limit 20, offset 0
	offsetRaw := strconv.Itoa(limitProcessPerExecution * currIter)
	audiences, err := getAudienceByCampaignId(db, campaignIdSource, limitProcessPerExecutionRaw, offsetRaw)
	if err != nil {
		fmt.Println("error get source audiences: ", err)
		return
	}

	// send 20 audience from campaignAudience source to campaignAudience dest
	// foreach data, call api campaignAudience add
	for _, v := range audiences {
		fmt.Println("WHO IS THE AUDIENCE: ", v)
		callLanggengAddAudience(cpmgnCodeDest, v.Name, v.Phone, `{"nama":"`+v.Name+`"}`, apiKey)
		randDelay := random(30, 60)
		fmt.Println("rand delay: ", randDelay)
		time.Sleep(time.Duration(randDelay) * time.Second)
	}

	nextIter, err := nextIter(currIter)
	if err != nil {
		return
	}

	fmt.Println(nextIter)
}

func callLanggengAddAudience(campaign_code, name, phone, customData, apikey string) {
	hc := http.Client{}

	form := url.Values{}
	form.Add("campaign_code", campaign_code)
	form.Add("name", name)
	form.Add("phone", phone)
	form.Add("custom_data", customData)
	form.Add("instant", "true")

	req, err := http.NewRequest("POST", apiHost+"api/_partners/campaigns/subscribe-by-code", strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("Request Failed: %s", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Api-Key", apikey)
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("Request Failed: %s", err)
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Body Read Failed: %s", err)
		return
	}
	// Log the request body
	bodyString := string(body)
	log.Print(bodyString)

	// Unmarshal result
	var best map[string]interface{}
	err = json.Unmarshal(body, &best)
	if err != nil {
		log.Printf("Reading body failed: %s", err)
		return
	}

	fmt.Println(best)
}

func getUserIdByApiKey(db *sql.DB, apiKey string) (string, error) {
	rows, err := db.Query("select id from users WHERE api_key = '" + apiKey + "'")

	if err != nil {
		fmt.Println("fail on query : ", err)
		return "", err
	}

	var userId string
	for rows.Next() {
		rows.Scan(&userId)
	}

	rows.Close()
	return userId, nil
}

func getCampaignIdByCodeUserid(db *sql.DB, code, userid string) (string, error) {
	rows, err := db.Query("select id from campaigns WHERE code = '" + code + "' AND user_id = " + userid)

	if err != nil {
		fmt.Println("fail on query : ", err)
		return "", err
	}

	var campaignId string
	for rows.Next() {
		rows.Scan(&campaignId)
	}

	rows.Close()
	return campaignId, nil
}

func getAudienceByCampaignId(db *sql.DB, campaignId string, limit, offset string) ([]Audience, error) {
	rows, err := db.Query("select b.id, b.name, b.phone_number, b.user_id " +
		"from campaign_audiences a " +
		"JOIN audiences b ON a.audience_id = b.id " +
		"WHERE a.campaign_id = " + campaignId +
		" LIMIT " + limit + " OFFSET " + offset,
	)

	if err != nil {
		fmt.Println("fail on query : ", err)
		return []Audience{}, err
	}

	result := []Audience{}
	for rows.Next() {
		audience := Audience{}
		rows.Scan(&audience.ID, &audience.Name, &audience.Phone, &audience.UserID)
		result = append(result, audience)
	}

	rows.Close()
	return result, nil
}

func getCurrentIteration() (int, error) {
	data, err := ioutil.ReadFile("iteration.ini")
	if err != nil {
		fmt.Println(err)
	}

	// if it was successful in reading the file then
	// print out the contents as a string
	iterationRaw := string(data)
	iteration := iterationRaw[0:len([]rune(iterationRaw))]
	iterationNum, err := strconv.Atoi(clearString(iteration))
	if err != nil {
		fmt.Println("fail to parse string into inteeger iteration")
		return 0, err
	}

	fmt.Println(iteration)
	return iterationNum, nil
}

func nextIter(iterationNum int) (int, error) {
	f, err := os.Create("iteration.ini")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	newIter := strconv.Itoa(iterationNum + 1)
	if _, err = f.WriteString(newIter); err != nil {
		fmt.Println("fail write to file for next iter num: ", err)
		return 0, err
	}

	return iterationNum + 1, nil
}

func random(min int, max int) int {
	return rand.Intn(max-min) + min
}

var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)

func clearString(str string) string {
	return nonAlphanumericRegex.ReplaceAllString(str, "")
}
