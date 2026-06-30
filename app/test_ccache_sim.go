package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheHost   = "localhost:28080"
	cachePrefix = "/cache/ccache"
)

var cppSource = []byte(`#include <iostream>
#include <vector>
#include <map>
#include <string>
#include <algorithm>
#include <numeric>
#include <cmath>
#include <random>

// Simulating a large C++ project with templates, math, and data structures
template<typename T, int N>
class Matrix {
    T data_[N][N];
public:
    Matrix() { for(int i=0;i<N;i++) for(int j=0;j<N;j++) data_[i][j]=0; }
    T& at(int i,int j){return data_[i][j];}
    T get(int i,int j)const{return data_[i][j];}
    Matrix operator*(const Matrix& o)const{
        Matrix r;
        for(int i=0;i<N;i++)
            for(int k=0;k<N;k++)
                for(int j=0;j<N;j++)
                    r.at(i,j)+=get(i,k)*o.get(k,j);
        return r;
    }
};

double heavyComputation(int n) {
    double sum=0;
    for(int i=0;i<n;i++){
        for(int j=0;j<n;j++){
            sum += std::sin(i)*std::cos(j)*std::sqrt(i+1);
        }
    }
    return sum;
}

template<typename T>
std::vector<T> processData(const std::vector<T>& input) {
    std::vector<T> result = input;
    std::sort(result.begin(), result.end());
    auto it = std::unique(result.begin(), result.end());
    result.erase(it, result.end());
    std::reverse(result.begin(), result.end());
    return result;
}

int main(){
    Matrix<double,128> m;
    for(int i=0;i<128;i++) m.at(i,i)=1.0;
    auto r = m*m*m;
    std::vector<double> data(10000);
    for(size_t i=0;i<data.size();i++) data[i]=i*0.5;
    auto processed = processData(data);
    std::cout<<"Sum="<<heavyComputation(2000)<<" mat="<<r.get(0,0)<<" proc="<<processed.size()<<"\n";
    return 0;
}
`)

func cacheURL(key string) string {
	return "http://" + cacheHost + cachePrefix + "/" + key
}

func computeHash(source []byte, args string) string {
	h := sha256.New()
	h.Write(source)
	h.Write([]byte(args))
	h.Write([]byte("#preprocessed#"))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func cacheDelete(key string) {
	req, _ := http.NewRequest("DELETE", cacheURL(key), nil)
	client := &http.Client{Timeout: 2 * time.Second}
	client.Do(req)
}

func cacheHead(key string) (bool, time.Duration) {
	start := time.Now()
	resp, err := http.Head(cacheURL(key))
	if err != nil {
		return false, time.Since(start)
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200, time.Since(start)
}

func cacheGet(key string) ([]byte, bool, time.Duration) {
	start := time.Now()
	resp, err := http.Get(cacheURL(key))
	if err != nil {
		return nil, false, time.Since(start)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false, time.Since(start)
	}
	data, _ := io.ReadAll(resp.Body)
	return data, true, time.Since(start)
}

func cachePut(key string, data []byte) (bool, time.Duration) {
	start := time.Now()
	req, _ := http.NewRequest("PUT", cacheURL(key), bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, time.Since(start)
	}
	resp.Body.Close()
	return resp.StatusCode == 201, time.Since(start)
}

func simulateCompile() int {
	// Simulate heavy C++ compilation: preprocessor + multiple optimization passes
	total := 0
	for i := 0; i < 500_000_000; i++ {
		total += i * 13 % 257
		if i%1000 == 0 {
			total ^= i >> 3
		}
	}

	// Simulate template instantiation and heavy optimization
	arr := make([]float64, 500_000)
	for i := range arr {
		arr[i] = float64(i) * 1.6180339887
	}
	for r := 0; r < 200; r++ {
		if r%2 == 0 {
			for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
				arr[i], arr[j] = arr[j], arr[i]
			}
		} else {
			for i := 0; i < len(arr)-1; i++ {
				if arr[i] > arr[i+1] {
					arr[i], arr[i+1] = arr[i+1], arr[i]
				}
			}
		}
	}

	// Simulate heavy math (like sin/cos loops in C++ code)
	sum := 0.0
	for i := 0; i < 2000; i++ {
		for j := 0; j < 2000; j++ {
			sum += float64(i)*0.01 + float64(j)*0.001
			if i%100 == 0 {
				sum *= 1.0001
			}
		}
	}

	return total + int(sum) + len(arr)
}

func main() {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("CCACHE Remote Cache Simulation")
	fmt.Printf("Cache server: http://%s\n", cacheHost)
	fmt.Println(strings.Repeat("=", 60))

	resp, err := http.Get("http://" + cacheHost + "/health")
	if err != nil || resp.StatusCode != 200 {
		fmt.Printf("ERROR: cache server not reachable: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
	fmt.Println("Cache server health: OK")

	tmpDir, _ := os.MkdirTemp("", "ccache_sim_")
	defer os.RemoveAll(tmpDir)

	sourcePath := filepath.Join(tmpDir, "main.cpp")
	os.WriteFile(sourcePath, cppSource, 0644)
	fmt.Printf("\nC++ source: %s (%d bytes)\n", sourcePath, len(cppSource))

	compilerArgs := "g++ -O2 -std=c++17 -c main.cpp -o main.o"
	hashKey := computeHash(cppSource, compilerArgs)
	fmt.Printf("Cache key: %s...\n", hashKey[:16])

	cacheDelete(hashKey)
	fmt.Println("Cache cleared for clean test")

	// --- Scenario 1: No cache ---
	fmt.Println("\n[1] NO CACHE — compile from scratch, no remote cache")
	start := time.Now()
	simulateCompile()
	objData := append([]byte("ELF\x00"), []byte(hashKey[:32])...)
	objData = append(objData, bytes.Repeat([]byte("A"), 8192)...)
	tNoCache := time.Since(start)
	fmt.Printf("  Compile time: %.3fs\n", tNoCache.Seconds())
	fmt.Printf("  Output size: %d bytes (~8 KB realistic .o file)\n", len(objData))

	// --- Scenario 2: Cold cache ---
	fmt.Println("\n[2] COLD CACHE — first build, compile + upload to remote cache")
	start = time.Now()

	hit, checkTime := cacheHead(hashKey)
	status := "MISS"
	if hit {
		status = "HIT"
	}
	fmt.Printf("  Cache HEAD check: %.2fms — %s\n", checkTime.Seconds()*1000, status)

	compileStart := time.Now()
	simulateCompile()
	compileTime := time.Since(compileStart)

	ok, uploadTime := cachePut(hashKey, objData)
	upStatus := "OK"
	if !ok {
		upStatus = "FAIL"
	}
	fmt.Printf("  Upload to remote cache: %.2fms — %s\n", uploadTime.Seconds()*1000, upStatus)

	tColdCache := time.Since(start)
	fmt.Printf("  Compile time: %.3fs\n", compileTime.Seconds())
	fmt.Printf("  Total time:   %.3fs\n", tColdCache.Seconds())
	fmt.Printf("  Cache overhead: %.2fms\n", (tColdCache - compileTime).Seconds()*1000)

	// --- Scenario 3: Hot cache ---
	fmt.Println("\n[3] HOT CACHE — cache hit, download from remote, skip compilation")
	start = time.Now()

	hit, checkTime = cacheHead(hashKey)
	status = "MISS"
	if hit {
		status = "HIT"
	}
	fmt.Printf("  Cache HEAD check: %.2fms — %s\n", checkTime.Seconds()*1000, status)

	if !hit {
		fmt.Println("  WARNING: expected HIT but got MISS!")
		compileStart = time.Now()
		simulateCompile()
		fmt.Printf("  Compile time: %.3fs\n", time.Since(compileStart).Seconds())
		tHotCache := time.Since(start)
		fmt.Printf("  Total time: %.3fs\n", tHotCache.Seconds())
	} else {
		data, ok, downloadTime := cacheGet(hashKey)
		dlStatus := "OK"
		if !ok {
			dlStatus = "FAIL"
		}
		fmt.Printf("  Download from remote cache: %.2fms — %s\n", downloadTime.Seconds()*1000, dlStatus)
		if ok {
			fmt.Printf("  Output size: %d bytes\n", len(data))
		}
		fmt.Println("  Compile skipped: YES (cache hit)")
		tHotCache := time.Since(start)
		fmt.Printf("  Total time: %.3fs\n", tHotCache.Seconds())

		// --- Summary ---
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("RESULTS SUMMARY")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("%-20s %10s %10s\n", "Scenario", "Time (s)", "Speedup")
		fmt.Println(strings.Repeat("-", 42))
		fmt.Printf("%-20s %10.3f %10s\n", "No cache", tNoCache.Seconds(), "1.00x")
		fmt.Printf("%-20s %10.3f %9.2fx\n", "Cold cache", tColdCache.Seconds(), tNoCache.Seconds()/tColdCache.Seconds())
		fmt.Printf("%-20s %10.3f %9.2fx\n", "Hot cache", tHotCache.Seconds(), tNoCache.Seconds()/tHotCache.Seconds())
		fmt.Println(strings.Repeat("-", 42))
		fmt.Printf("\nHot cache vs no cache:  %.0fx faster\n", tNoCache.Seconds()/tHotCache.Seconds())
		fmt.Printf("Hot cache vs cold cache:  %.0fx faster\n", tColdCache.Seconds()/tHotCache.Seconds())
		fmt.Printf("Cold cache overhead:      %.2fms (%.1f%% of compile time)\n",
			(tColdCache - tNoCache).Seconds()*1000,
			(tColdCache - tNoCache).Seconds()/tNoCache.Seconds()*100)
		fmt.Printf("Remote cache latency:     %.2fms HEAD + %.2fms GET\n",
			checkTime.Seconds()*1000, downloadTime.Seconds()*1000)
	}
	fmt.Println(strings.Repeat("=", 60))
}
