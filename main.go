package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/joho/godotenv"
)

type MorphRequestBody struct {
	Count    int
	FileName string
	Body     map[string]any
}

type CsvBody struct {
	header []string
	body   []string
}

type LookupRowShort struct {
	Description string `json:"description"`
	Example     string `json:"example"`
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	var port string
	port = fmt.Sprintf(":%s", os.Getenv("PORT"))
	if port == ":" {
		port = ":8080"
	}

	routes()
	log.Printf("Running %s \n", port)
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("Error starting %s", port)
	}
}

func routes() {

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{})
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Init(w, r)
	})

	http.HandleFunc("/to-csv", func(w http.ResponseWriter, r *http.Request) {
		// InitOld(w, r)
		// Init(w, r)
		InitCsv(w, r)
	})

	http.HandleFunc("/fullHelp", func(w http.ResponseWriter, r *http.Request) {
		sendResponse(lookupFull(), w)
	})

	http.HandleFunc("/help", func(w http.ResponseWriter, r *http.Request) {
		sendResponse(lookup(), w)
	})

}

func InitCsv(w http.ResponseWriter, r *http.Request) {
	request, err := getRequestBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	csvResults := [][]string{}
	// numWorkers := runtime.NumCPU()
	// jobs := make(chan int, request.Count)
	// var wg sync.WaitGroup

	//first get the headers
	headers := []string{}
	for key, _ := range request.Body {
		headers = append(headers, key)
	}

	sort.Strings(headers)
	csvResults = append(csvResults, headers)
	//then append the items
	for i := 0; i < request.Count; i++ {
		csvRow, err := createFakeCsvEntry(request.Body, headers)
		if err != nil {
			http.Error(w, "CSV error", 500)
		}

		csvResults = append(csvResults, csvRow)
	}

	generateCSV(csvResults, request)
}

func Init(w http.ResponseWriter, r *http.Request) {

	request, err := getRequestBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}

	fakeResults := make([]map[string]any, request.Count)
	numWorkers := runtime.NumCPU()
	jobs := make(chan int, request.Count)
	var wg sync.WaitGroup

	for worker := 1; worker <= numWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker listens for a job (an index) from the channel
			for i := range jobs {
				// Generate the fake data
				data, err := createFake(request.Body)
				if err != nil {
					// In a real app, handle this error via a channel
					http.Error(w, "Generation error", 500)
					continue
				}
				// Store the result at the specific index
				fakeResults[i] = data
			}
		}()
	}

	// 5. Feed the jobs into the channel
	for i := 0; i < request.Count; i++ {
		jobs <- i
	}
	close(jobs) // Closing the channel tells workers to stop when done

	// 6. Wait for all workers to finish
	wg.Wait()

	sendResponse(fakeResults, w)
}

func getRequestBody(w http.ResponseWriter, r *http.Request) (*MorphRequestBody, error) {
	var requestData map[string]any

	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		return nil, fmt.Errorf("Invalid JSON")
	}
	defer r.Body.Close()

	countVal, ok := requestData["count"].(float64)
	if !ok {
		return nil, fmt.Errorf("Invalid Count")
	}
	count := int(countVal)

	fileName, ok := requestData["fileName"].(string)
	if !ok {
		return nil, fmt.Errorf("Invalid Name")
	}

	body, ok := requestData["body"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Invalid Body")
	}

	morphRequest := MorphRequestBody{
		Count:    count,
		FileName: fileName,
		Body:     body,
	}

	return &morphRequest, nil
}

func sendResponse(fakeData any, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(fakeData)
}

func createFakeCsvEntry(body map[string]any, sortedHeaders []string) ([]string, error) {
	// Pre-allocate slice for better performance
	fakeData := make([]string, 0, len(sortedHeaders))

	for _, header := range sortedHeaders {
		// 1. LOOK UP the value in the map using the header key
		val, exists := body[header]
		if !exists {
			fakeData = append(fakeData, "") // Column exists in headers but not in body
			continue
		}

		// 2. Type switch on the VALUE found in the map (which is 'any')
		switch keywordValue := val.(type) {
		case string:
			// nulls
			if keywordValue == "null" {
				fakeData = append(fakeData, "")
				continue
			}

			// lexify
			if keywordValue == "?" {
				fakeData = append(fakeData, gofakeit.Lexify("?"))
				continue
			}

			// numerify
			if keywordValue == "#" {
				fakeData = append(fakeData, gofakeit.Numerify("#"))
				continue // Added continue to prevent it from falling into parseKey
			}

			// randomstring
			if strings.HasPrefix(keywordValue, "randomstring:") {
				listStr := keywordValue[len("randomstring:"):]
				options := strings.Split(listStr, ",")
				fakeData = append(fakeData, gofakeit.RandomString(options))
				continue
			}

			if strings.HasPrefix(keywordValue, "daterange:") {
				listStr := keywordValue[len("daterange:"):]
				options := strings.Split(listStr, ",")
				layout := "2006-01-02"
				startStr, err := time.Parse(layout, options[0])
				if err != nil {
					return nil, fmt.Errorf("Invalid date range")
				}

				endStr, err := time.Parse(layout, options[1])
				if err != nil {
					return nil, fmt.Errorf("Invalid date range")
				}
				drange := gofakeit.DateRange(startStr, endStr)
				fakeData = append(fakeData, drange.String())
			}

			// Custom Key Parsing (e.g., "FirstName", "Email")
			fakeValue, err := parseKey(keywordValue)
			if err != nil {
				return nil, fmt.Errorf("keyword %s for header %s is not supported", keywordValue, header)
			}

			fakeValue = cleanFakeOutput(fakeValue)
			fakeData = append(fakeData, fakeValue)

		default:
			// If the map contains something that isn't a string (like an int or bool)
			// Convert it to a string so it can fit in the CSV
			fakeData = append(fakeData, fmt.Sprintf("%v", val))
		}
	}

	return fakeData, nil
}

func createFake(body map[string]any) (map[string]any, error) {
	fakeData := make(map[string]any)
	for k, v := range body {

		switch keywordValue := v.(type) {
		case string:

			// nulls
			if keywordValue == "null" {
				fakeData[k] = nil
				continue
			}

			// lexify
			if keywordValue == "?" {
				fakeData[k] = gofakeit.Lexify("?")
				continue
			}

			if keywordValue == "#" {
				fakeData[k] = gofakeit.Numerify("#")
			}

			//randomstring
			if strings.HasPrefix(keywordValue, "randomstring:") {
				listStr := keywordValue[len("randomstring:"):]
				options := strings.Split(listStr, ",")
				fakeData[k] = gofakeit.RandomString(options)
				continue
			}

			if strings.HasPrefix(keywordValue, "daterange:") {
				listStr := keywordValue[len("daterange:"):]
				options := strings.Split(listStr, ",")
				layout := "2006-01-02"
				startStr, err := time.Parse(layout, options[0])
				if err != nil {
					return nil, fmt.Errorf("Invalid date range")
				}

				endStr, err := time.Parse(layout, options[1])
				if err != nil {
					return nil, fmt.Errorf("Invalid date range")
				}

				fakeData[k] = gofakeit.DateRange(startStr, endStr).String()
			}

			fakeValue, err := parseKey(keywordValue)
			if err != nil {
				return nil, fmt.Errorf("Keyword %s is not supported", keywordValue)
			}

			fakeValue = cleanFakeOutput(fakeValue)
			fakeData[k] = fakeValue

		default:
			return nil, fmt.Errorf("Keyword %s is not supported", keywordValue)
		}
	}

	return fakeData, nil
}

func cleanFakeOutput(fData string) string {
	if strings.Contains(fData, "{") {
		res := strings.TrimPrefix(fData, "{")
		res = strings.TrimSuffix(res, "}")
		return res
	}
	return fData
}

func parseKey(key string) (string, error) {
	formattedKey := "{" + key + "}"
	return gofakeit.Generate(formattedKey)
}

func lookupFull() map[string]gofakeit.Info {
	return gofakeit.FuncLookups
}

func lookup() map[string]LookupRowShort {
	lookups := make(map[string]LookupRowShort)
	lookupJson := gofakeit.FuncLookups

	for k, v := range lookupJson {
		lookups[k] = LookupRowShort{
			Description: v.Description,
			Example:     v.Example,
		}
	}

	return lookups
}

func generateCSV(csvData [][]string, request *MorphRequestBody) {
	// 1. Create the file
	file, err := os.Create(request.FileName + ".csv")
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	csvWriter := csv.NewWriter(file)
	defer csvWriter.Flush()

	// 4. Write all data at once
	if err := csvWriter.WriteAll(csvData); err != nil {
		fmt.Println("Error writing to csv:", err)
	}

	fmt.Printf("\nCSV file ( %s ) created successfully!", request.FileName)
}
