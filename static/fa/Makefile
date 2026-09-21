.PHONY: all virtualenv clean

all: virtualenv
	ve/bin/python3 minimize.py icons.spec --url /static/fa/icons.woff2

virtualenv:
	@if ! ve/bin/python3 -c "import fontTools" >/dev/null 2>&1; then \
		python3 -m venv ve && ve/bin/pip install -r requirements.txt; \
	fi

clean:
	rm -f icons.css icons.woff2
	rm -rf ve
