# Lambda binaries: linux/arm64 (Graviton), named `bootstrap` for provided.al2023.
.PHONY: build test fmt clean plan deploy-publisher publisher-build itch

build:
	cd services/ad-server && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		go build -tags lambda -ldflags="-s -w" -o build/bootstrap .
	cd services/cost-fuse && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		go build -ldflags="-s -w" -o build/bootstrap .
	@ls -lh services/*/build/bootstrap

test:
	cd services/ad-server && go test ./... -count=1

fmt:
	gofmt -w services/
	terraform fmt -recursive infrastructure/

plan: build
	cd infrastructure/ad-server && terraform plan -no-color

# Account-specific values live in local.mk (gitignored), never here:
#   AWS_PROFILE_NAME = my-profile
#   AWS_ACCOUNT_ID   = 000000000000
# No fallback to the default profile on purpose: on a machine with several
# accounts the default is the wrong one.
-include local.mk
AWS_PROFILE_NAME ?= $(error set AWS_PROFILE_NAME in local.mk)
AWS_ACCOUNT_ID   ?= $(error set AWS_ACCOUNT_ID in local.mk)
SITE_BUCKET ?= adtech-lab-publisher-$(AWS_ACCOUNT_ID)
AWS         ?= aws --profile $(AWS_PROFILE_NAME) --region us-east-1

# Content-hash the assets before deploying. Immutable caching on a STABLE
# filename means a visitor who loaded the old file never gets the new one, so
# the names have to change when the content does.
publisher-build:
	python3 tools/build/hash-assets.py

# Content is a deploy step, not Terraform state: `terraform destroy` must never
# be able to take the site's files with it.
#
# Two passes, and the ORDER matters. Hashed assets go up first so that a visitor
# who fetches new HTML can never reference an asset that is not there yet.
# `--delete` is deliberately absent on the asset pass: old hashed files stay
# until the HTML that references them has aged out of caches.
deploy-publisher: publisher-build
	$(AWS) s3 sync apps/publisher/build/static/ s3://$(SITE_BUCKET)/static/ \
		--cache-control "public,max-age=31536000,immutable"
	$(AWS) s3 sync apps/publisher/build/ s3://$(SITE_BUCKET)/ \
		--exclude "static/*" --delete \
		--cache-control "public,max-age=300"
	$(AWS) cloudfront create-invalidation \
		--distribution-id $(shell $(AWS) cloudfront list-distributions \
			--query "DistributionList.Items[?Aliases.Items[0]=='xoxoxo.live'].Id | [0]" --output text) \
		--paths "/*" >/dev/null
	@echo "deployed. HTML invalidated; hashed assets need no invalidation."

# Standalone, self-contained game bundles for itch.io.
itch:
	python3 tools/build/itch-package.py

clean:
	rm -rf dist/itch services/*/build infrastructure/*/build apps/publisher/build

test-site: ## Publisher regression test -- run after every game change
	python3 tools/build/hash-assets.py >/dev/null
	python3 tools/test/site_check.py

# Terraform cannot use the `login_session` profile format the AWS CLI v2 writes,
# so credentials are exported into the environment for it. Everything else in
# this Makefile uses the CLI directly and does not need this.
tf-env:
	@aws configure export-credentials --profile $(AWS_PROFILE_NAME) --format env

# --- Phase 4: two services, one boundary ---
dsp: ## run the mini-DSP (the buyer) on :8081
	cd services/mini-dsp && go run . -addr :8081 -campaigns campaigns.json -nurl-loss 0.15 -v

ad-server-local: ## run the ad server (the seller) on :8090, lab mode
	cd services/ad-server && ADLAB_ADDR=:8090 ADLAB_ENV=lab ADLAB_TMAX_MS=120 go run -tags local .

discrepancy: ## drive traffic through both and compare their numbers
	python3 tools/dev/discrepancy.py

# Terraform cannot read the CLI's `login_session` profile format, so
# credentials are exported into the environment for it. See tf-env.
TF_ENV = eval "$$(aws configure export-credentials --profile $(AWS_PROFILE_NAME) --format env)"

deploy-ad-server: build test ## build, test, then apply the ad-server stack
	@echo "--- plan ---"
	cd infrastructure/ad-server && $(TF_ENV) && terraform plan -no-color -var 'profile=' -out=/tmp/adserver.tfplan
	@echo
	@read -p "apply this plan? [y/N] " ok; [ "$$ok" = "y" ] || { echo "aborted"; exit 1; }
	cd infrastructure/ad-server && $(TF_ENV) && terraform apply -no-color /tmp/adserver.tfplan

plan-ad-server: build ## plan only, no prompt
	cd infrastructure/ad-server && $(TF_ENV) && terraform plan -no-color -var 'profile='

verify-supplychain: ## walk our own schain the way a buyer would
	cd tools/supplychain && go run . --local --root ../..

check-prod: ## sweep the LIVE site -- run before sending anyone to it
	python3 tools/test/prod_check.py

og: ## regenerate the Open Graph preview image
	python3 tools/build/og/render.py
