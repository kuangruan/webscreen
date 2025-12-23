.PHONY: all build-capturer build-main clean

# 默认目标：输入 make 就会执行这个
all: build-capturer build-main

# 构建 capturer
build-capturer:
	@echo "building Capturer..."
	go build -o sdriver/x11/bin/capturer_xvfb ./capturer

# 构建主程序
build-main:
	@echo "building main program..."
	go build -o webscreen .

# 清理
clean:
	rm -f capturer_xvfb_bin webscreen