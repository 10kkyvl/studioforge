{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.CkSbSjKU.js"),
						import("/_app/immutable/entry/app.D6v8PpEG.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			