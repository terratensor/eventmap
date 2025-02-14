package main

import (
    "encoding/json"
    "fmt"
    "html"
    "io/ioutil"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/atotto/clipboard"
)

const (
    outputDir = "C:\\Projects\\eventmap"
)

type YandexGeocodeResponse struct {
    Response struct {
        GeoObjectCollection struct {
            FeatureMember []struct {
                GeoObject struct {
                    Name string `json:"name"`
                    Point struct {
                        Pos string `json:"pos"`
                    } `json:"Point"`
                } `json:"GeoObject"`
            } `json:"featureMember"`
        } `json:"GeoObjectCollection"`
    } `json:"response"`
}

func readAPIKey() (string, error) {
    keyBytes, err := ioutil.ReadFile("apikey.txt")
    if err != nil {
        return "", fmt.Errorf("ошибка чтения файла с API-ключом: %v", err)
    }
    apiKey := strings.TrimSpace(string(keyBytes))
    if apiKey == "" {
        return "", fmt.Errorf("файл apikey.txt пуст")
    }
    return apiKey, nil
}

func sanitizeFileName(name string) string {
    forbiddenChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|", " "}
    for _, char := range forbiddenChars {
        name = strings.ReplaceAll(name, char, "_")
    }
    return name
}

func extractToponym(urlFragment string) (string, error) {
    if matches := regexp.MustCompile(`(?i)-,([^,]+?),-`).FindStringSubmatch(urlFragment); len(matches) >= 2 {
        return url.QueryUnescape(matches[1])
    }
    decoded, err := url.QueryUnescape(urlFragment)
    if err != nil {
        return "", fmt.Errorf("ошибка декодирования URL: %v", err)
    }
    if idx := strings.Index(decoded, "&"); idx != -1 {
        decoded = decoded[:idx]
    }
    return decoded, nil
}

func main() {
    fmt.Println("[1/8] Загрузка API-ключа...")
    apiKey, err := readAPIKey()
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    fmt.Println("[2/8] Проверка выходной директории...")
    if _, err := os.Stat(outputDir); os.IsNotExist(err) {
        fmt.Printf("Создаём директорию: %s\n", outputDir)
        if err := os.MkdirAll(outputDir, 0755); err != nil {
            fmt.Fprintf(os.Stderr, "Ошибка создания директории: %v\n", err)
            os.Exit(1)
        }
    }

    fmt.Println("[3/8] Чтение буфера обмена...")
    clipboardContent, err := clipboard.ReadAll()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Ошибка чтения буфера: %v\n", err)
        os.Exit(1)
    }
    if clipboardContent == "" {
        fmt.Fprintln(os.Stderr, "Буфер обмена пуст")
        os.Exit(1)
    }
    fmt.Printf("Получено содержимое буфера: %s\n", clipboardContent)

    fmt.Println("[4/8] Извлечение топонима...")
    parts := strings.Split(clipboardContent, "#:~:text=")
    if len(parts) < 2 {
        fmt.Fprintln(os.Stderr, "Некорректный формат текстового фрагмента")
        os.Exit(1)
    }
    toponym, err := extractToponym(parts[1])
    if err != nil {
        fmt.Fprintf(os.Stderr, "Ошибка извлечения топонима: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("Топоним: '%s'\n", toponym)

    fmt.Println("[5/8] Обработка имени файла...")
    encodedToponym := url.QueryEscape(toponym)
    apiURL := fmt.Sprintf(
        "https://geocode-maps.yandex.ru/1.x/?format=json&apikey=%s&geocode=%s",
        apiKey,
        encodedToponym,
    )
    fmt.Printf("[6/8] Запрос к API...\nURL: %s\n", apiURL)
    resp, err := http.Get(apiURL)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Ошибка запроса: %v\n", err)
        os.Exit(1)
    }
    defer resp.Body.Close()

    fmt.Printf("Статус ответа: %s\n", resp.Status)
    if resp.StatusCode != http.StatusOK {
        body, _ := ioutil.ReadAll(resp.Body)
        fmt.Fprintf(os.Stderr, "Ошибка API: %d\nОтвет: %s\n", resp.StatusCode, body)
        os.Exit(1)
    }

    fmt.Println("[7/8] Обработка ответа...")
    var yandexResp YandexGeocodeResponse
    if err := json.NewDecoder(resp.Body).Decode(&yandexResp); err != nil {
        fmt.Fprintf(os.Stderr, "Ошибка парсинга JSON: %v\n", err)
        os.Exit(1)
    }
    if len(yandexResp.Response.GeoObjectCollection.FeatureMember) == 0 {
        fmt.Fprintln(os.Stderr, "Топоним не найден")
        os.Exit(1)
    }
    fmt.Printf("Найдено результатов: %d\n", len(yandexResp.Response.GeoObjectCollection.FeatureMember))

    // Use the 'Name' field from the API response for the KML file name
    yandexToponym := yandexResp.Response.GeoObjectCollection.FeatureMember[0].GeoObject.Name
    if yandexToponym == "" {
        fmt.Fprintln(os.Stderr, "Имя топонима отсутствует в ответе API")
        os.Exit(1)
    }
    sanitized := sanitizeFileName(yandexToponym)
    filename := filepath.Join(outputDir, sanitized+".kml")

    pos := yandexResp.Response.GeoObjectCollection.FeatureMember[0].GeoObject.Point.Pos
    coords := strings.Split(pos, " ")
    if len(coords) != 2 {
        fmt.Fprintln(os.Stderr, "Неверный формат координат")
        os.Exit(1)
    }
    longitude, latitude := coords[0], coords[1]
    fmt.Printf("Координаты: долгота=%s, широта=%s\n", longitude, latitude)

    fmt.Println("[8/8] Создание KML...")
    escapedURL := html.EscapeString(clipboardContent)
    iframeContent := fmt.Sprintf(
        `<iframe src="%s" width="700" height="800" frameborder="0"></iframe>`,
        escapedURL,
    )
    kmlTemplate := `<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2">
	<Document>
		<Style id="customIcon">
			<IconStyle>
				<color>ffc3ff82</color>
				<scale>1.3</scale>
				<Icon>
					<href>https://static.svodd.ru/geomatrix/icons/BRICK.png</href>
				</Icon>
			</IconStyle>
			<LabelStyle>
				<scale>0</scale>
			</LabelStyle>
			<ListStyle>
			</ListStyle>
		</Style>
		<Placemark>
			<name>%s</name>
			<description><![CDATA[%s]]></description>
			<styleUrl>#customIcon</styleUrl>
			<Point>
				<coordinates>%s,%s,0</coordinates>
			</Point>
		</Placemark>
	</Document>
</kml>`
    kmlContent := fmt.Sprintf(kmlTemplate, yandexToponym, iframeContent, longitude, latitude)
    fmt.Printf("[9/8] Запись в '%s'...\n", filename)
    if err := ioutil.WriteFile(filename, []byte(kmlContent), 0644); err != nil {
        fmt.Fprintf(os.Stderr, "Ошибка записи: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("Готово! KML-файл успешно создан в целевой директории.")
}